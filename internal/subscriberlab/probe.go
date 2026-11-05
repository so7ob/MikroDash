package subscriberlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
	"layeh.com/radius/rfc3576"
	"mikrodash/internal/db"
	"mikrodash/internal/routeros"
	"mikrodash/internal/subscriber"
)

type recorder struct {
	mu       sync.Mutex
	w        io.Writer
	writeErr error
}

func (r *recorder) emit(step string, value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeErr != nil {
		return
	}
	r.writeErr = json.NewEncoder(r.w).Encode(struct {
		Synthetic bool      `json:"synthetic"`
		At        time.Time `json:"at"`
		Step      string    `json:"step"`
		Value     any       `json:"value"`
	}{true, time.Now().UTC(), step, value})
}

func (r *recorder) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writeErr
}

// ProtocolProbe sends traffic from the real namespace client through CHR. The
// production PAP/accounting implementation owns a new, durable synthetic ledger.
// Manual CoA probes deliberately measure semantics before enabling capabilities;
// they do not claim that the normal worker renewed its persisted reservations.
func (c *Client) ProtocolProbe(ctx context.Context, privateDir string, out io.Writer, https bool, scenario string) error {
	if _, err := c.Identity(); err != nil {
		return err
	}
	var fixture FixtureCredentials
	if err := privateJSON(filepath.Join(privateDir, "fixture.json"), &fixture); err != nil {
		return err
	}
	if len(fixture.Secret) < 32 || len(fixture.Password) < 24 {
		return errors.New("random private fixture credentials required")
	}
	if err := c.clearPreviousProbe(ctx, privateDir, https); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(privateDir, "probe-")
	if err != nil {
		return err
	}
	database, err := db.Open(dir)
	if err != nil {
		return err
	}
	defer database.Close()
	store := database.Subscribers()
	account, _, err := store.CreateAccount(ctx, "test:subscriber-chr", "chr-lab", "Synthetic CHR protocol probe", fixture.Password, "isolated protocol lab", 0)
	if err != nil {
		return err
	}
	now := time.Now()
	plan := subscriber.Plan{ID: "chr-lab", Version: 1, Name: "Synthetic CHR plan", Start: "first-login", ValidSeconds: 86400,
		MaxSessions: 1, Networks: []string{"C"}, Down: 512000, Up: 128000,
		Speeds: []subscriber.Speed{{ID: "economy", Name: "Economy", Down: 128000, Up: 64000}, {ID: "fast", Name: "Fast", Down: 512000, Up: 128000}},
		Policies: []subscriber.Policy{{ID: "all", Limits: []subscriber.Limit{
			{ID: "time", Kind: "seconds", State: "limited", Amount: 3600, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: now}},
			{ID: "download", Kind: "download", State: "limited", Amount: 16 << 20, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: now}},
			{ID: "upload", Kind: "upload", State: "limited", Amount: 4 << 20, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: now}},
		}}}}
	if scenario == "download-limit" {
		plan.Policies[0].Limits[1].Amount = 65536
	} else if scenario == "upload-limit" {
		plan.Policies[0].Limits[2].Amount = 65536
	} else if scenario == "byte-renew" {
		plan.Speeds[0], plan.Speeds[1] = plan.Speeds[1], plan.Speeds[0]
	} else if scenario != "timeout" && scenario != "disconnect" {
		return errors.New("unknown protocol scenario")
	}
	if err = store.SavePlan(ctx, "test:subscriber-chr", "isolated protocol lab", plan); err != nil {
		return err
	}
	if _, err = store.Assign(ctx, "test:subscriber-chr", account.ID, plan.ID, 1, "isolated protocol lab", now); err != nil {
		return err
	}
	service := &subscriber.RadiusService{Store: store, Mode: "pilot", PilotAccounts: map[string]bool{account.ID: true},
		Clients: []subscriber.NAS{{RouterID: "chr-lab", Network: "C", Address: "127.0.0.1", CalledStationID: "hs-lab", Secret: fixture.Secret, DisconnectPort: 13799}}}
	record := &recorder{w: out}
	starts := make(chan string, 1)
	handler := radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		service.ServeRADIUS(w, r)
		if r.Packet.Code != radius.CodeAccountingRequest {
			record.emit("access-request", map[string]string{"calledStationId": rfc2865.CalledStationID_GetString(r.Packet), "callingStationId": rfc2865.CallingStationID_GetString(r.Packet)})
			return
		}
		p := r.Packet
		record.emit("accounting-request", map[string]string{
			"status": fmt.Sprint(rfc2866.AcctStatusType_Get(p)), "sessionId": rfc2866.AcctSessionID_GetString(p),
			"seconds": fmt.Sprint(uint64(rfc2866.AcctSessionTime_Get(p))), "inputOctets": fmt.Sprint(uint64(rfc2866.AcctInputOctets_Get(p))), "outputOctets": fmt.Sprint(uint64(rfc2866.AcctOutputOctets_Get(p))),
			"inputGigawords": fmt.Sprint(uint64(rfc2869.AcctInputGigawords_Get(p))), "outputGigawords": fmt.Sprint(uint64(rfc2869.AcctOutputGigawords_Get(p))),
			"terminateCause": fmt.Sprint(rfc2866.AcctTerminateCause_Get(p)),
		})
		if rfc2866.AcctStatusType_Get(p) == rfc2866.AcctStatusType_Value_Start {
			select {
			case starts <- rfc2866.AcctSessionID_GetString(p):
			default:
			}
		}
	})
	for _, addr := range []string{"127.0.0.1:12812", "127.0.0.1:12813"} {
		conn, err := net.ListenPacket("udp", addr)
		if err != nil {
			return err
		}
		server := &radius.PacketServer{Handler: handler, SecretSource: service, ErrorLog: log.New(io.Discard, "", 0)}
		go server.Serve(conn)
		defer func() {
			stop, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			server.Shutdown(stop)
		}()
	}
	before, err := store.Snapshot(ctx, account.ID, time.Now())
	if err != nil {
		return err
	}
	record.emit("before-login", before)
	if err = verifyProbeBefore(before); err != nil {
		return err
	}
	if err = c.login(ctx, privateDir, fixture.Password, https); err != nil {
		return err
	}
	var session string
	select {
	case session = <-starts:
	case <-time.After(5 * time.Second):
		return errors.New("real HotSpot did not send Accounting-Start")
	case <-ctx.Done():
		return ctx.Err()
	}
	started := time.Now()
	read := func(step string) ([]routeros.Reply, error) {
		rows, err := c.Do(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=.proplist=.id,server,user,address,mac-address,uptime,session-time-left,bytes-in,bytes-out,limit-bytes-in,limit-bytes-out,limit-bytes-total,login-by,radius"}, Timeout: 5 * time.Second})
		record.emit(step, rows)
		return rows, err
	}
	rows, err := read("initial-active")
	if err != nil || len(rows) != 1 || rows[0]["user"] != "chr-lab" {
		return errors.New("unique synthetic active session not proven")
	}
	initialID := rows[0][".id"]
	if err = verifyTotalTimeout(rows[0], 60); err != nil {
		return err
	}
	if scenario == "download-limit" || scenario == "upload-limit" {
		return c.localByteProbe(ctx, privateDir, fixture.Password, https, scenario, store, account.ID, record)
	}
	if scenario == "byte-renew" {
		return c.byteRenewProbe(ctx, fixture.Secret, session, store, account.ID, record)
	}
	if scenario == "disconnect" {
		return c.disconnectProbe(ctx, service, store, account.ID, record)
	}
	if err = pause(ctx, 8*time.Second); err != nil {
		return err
	}
	if _, err = read("before-extension"); err != nil {
		return err
	}
	selector := "full"
	for _, candidate := range []string{"full", "full-ip", "no-called-ip", "minimal-ip"} {
		selector = candidate
		err = c.timeoutProbe(ctx, fixture.Secret, session, 90, selector, record)
		if err == nil {
			break
		}
	}
	if err != nil {
		return err
	}
	rows, err = read("after-extension")
	if err != nil || len(rows) != 1 || rows[0][".id"] != initialID {
		return errors.New("extension did not preserve the exact active session")
	}
	if err = verifyTotalTimeout(rows[0], 90); err != nil {
		return err
	}
	if err = c.rateProbe(ctx, fixture.Secret, session, 128000, 512000, record); err != nil {
		return err
	}
	if err = c.trafficProbe(ctx, record, 65536); err != nil {
		return err
	}
	if err = c.rateProbe(ctx, fixture.Secret, session, 64000, 128000, record); err != nil {
		return err
	}
	if err = pause(ctx, time.Until(started.Add(63*time.Second))); err != nil {
		return err
	}
	rows, err = read("past-original-timeout")
	if err != nil || len(rows) != 1 || rows[0][".id"] != initialID {
		return errors.New("CoA did not keep the original session past its admission timeout")
	}
	settledSample := rows[0]
	if err = c.timeoutProbe(ctx, fixture.Secret, session, 5, selector, record); err != nil {
		return err
	}
	rows, err = read("after-shortening")
	if err != nil || len(rows) != 0 {
		return errors.New("shortening below current uptime did not terminate a total-mode session")
	}
	if err = pause(ctx, 7*time.Second); err != nil {
		return err
	}
	rows, err = read("after-short-timeout")
	if err != nil || len(rows) != 0 {
		return errors.New("short local timeout did not end the synthetic session")
	}
	final, err := store.Snapshot(ctx, account.ID, time.Now())
	if err != nil {
		return err
	}
	record.emit("final-ledger", final)
	if err = verifyProbeFinal(before, final, settledSample, session); err != nil {
		return err
	}
	return record.Err()
}

func (c *Client) login(ctx context.Context, privateDir, password string, https bool) error {
	return c.loginAs(ctx, privateDir, "chr-lab", password, https)
}

func (c *Client) loginAs(ctx context.Context, privateDir, username, password string, https bool) error {
	if username != "chr-lab" && username != "legacy-lab" && username != `md-pilot\legacy-lab` && username != "legacy-lab@md-pilot" {
		return errors.New("synthetic lab username required")
	}
	if _, err := c.Identity(); err != nil {
		return err
	}
	args := []string{"netns", "exec", "md-subscriber-lab", "curl", "--noproxy", "*", "--max-time", "10", "--silent", "--show-error",
		"--data-urlencode", "username=" + username, "--data-urlencode", "password@-"}
	args = append(args, labHotSpotURL(privateDir, "/login", https)...)
	cmd := exec.CommandContext(ctx, "ip", args...)
	cmd.Stdin = strings.NewReader(password)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		return errors.New("isolated HotSpot client POST failed")
	}
	return nil
}

func (c *Client) clearPreviousProbe(ctx context.Context, privateDir string, https bool) error {
	return c.logoutClient(ctx, privateDir, "chr-lab", https)
}

func (c *Client) logoutClient(ctx context.Context, privateDir, username string, https bool) error {
	rows, err := c.Do(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=.proplist=user,server,mac-address"}, Timeout: 5 * time.Second})
	if err != nil || len(rows) > 1 {
		return errors.New("cannot prove empty or owned previous lab session")
	}
	if len(rows) == 0 {
		return nil
	}
	if (username != "chr-lab" && username != "legacy-lab") || rows[0]["user"] != username || rows[0]["server"] != "hs-lab" || rows[0]["mac-address"] != "02:00:00:00:54:02" {
		return errors.New("refusing to log out a different lab user or device")
	}
	args := []string{"netns", "exec", "md-subscriber-lab", "curl", "--noproxy", "*", "--max-time", "5", "--silent"}
	args = append(args, labHotSpotURL(privateDir, "/logout", https)...)
	cmd := exec.CommandContext(ctx, "ip", args...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		return errors.New("previous synthetic client logout failed")
	}
	return pause(ctx, time.Second)
}

func labHotSpotURL(privateDir, path string, https bool) []string {
	if https {
		return []string{"--cacert", filepath.Join(privateDir, "md-lab-ca.crt"), "--resolve", "login.example.test:443:198.51.100.1", "https://login.example.test" + path}
	}
	return []string{"http://198.51.100.1" + path}
}

func (c *Client) timeoutProbe(ctx context.Context, secret, session string, timeout uint32, selector string, record *recorder) error {
	if _, err := c.Identity(); err != nil {
		return err
	}
	p := radius.New(radius.CodeCoARequest, []byte(secret))
	for _, err := range []error{
		rfc2865.UserName_SetString(p, "chr-lab"), rfc2866.AcctSessionID_SetString(p, session),
		rfc2865.SessionTimeout_Set(p, rfc2865.SessionTimeout(timeout)),
	} {
		if err != nil {
			return err
		}
	}
	if strings.HasSuffix(selector, "-ip") {
		if err := rfc2865.FramedIPAddress_Set(p, net.IPv4(198, 51, 100, 2)); err != nil {
			return err
		}
	}
	if selector != "minimal-ip" {
		if err := rfc2865.CallingStationID_SetString(p, "02:00:00:00:54:02"); err != nil {
			return err
		}
		if err := rfc2869.EventTimestamp_Set(p, time.Now()); err != nil {
			return err
		}
	}
	if selector == "full" || selector == "full-ip" {
		if err := rfc2865.CalledStationID_SetString(p, "hs-lab"); err != nil {
			return err
		}
	}
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	client := radius.Client{MaxPacketErrors: 10}
	reply, err := client.Exchange(deadline, p, "127.0.0.1:13799")
	if err != nil {
		return errors.New("CHR CoA probe timed out")
	}
	record.emit("timeout-coa-response", map[string]any{"selector": selector, "requested": fmt.Sprint(timeout), "code": reply.Code.String(), "errorCause": fmt.Sprint(rfc3576.ErrorCause_Get(reply))})
	if reply.Code != radius.CodeCoAACK {
		return errors.New("CHR rejected timeout probe; inspect retained NAK")
	}
	return nil
}

func pause(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
