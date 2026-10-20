package subscriber

import (
	"context"
	"database/sql"
	"io"
	"log"
	"net"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
	"layeh.com/radius/vendors/mikrotik"
	_ "modernc.org/sqlite"
)

const testSecret = "synthetic-radius-test-secret-only"

func protocolFixtureURI(path string) string {
	u := url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=foreign_keys(1)&_txlock=immediate"}
	return u.String()
}

func protocolFixture(t *testing.T, configure ...func(*RadiusService)) (*Store, *RadiusService, Account, string) {
	t.Helper()
	h, err := sql.Open("sqlite", protocolFixtureURI(filepath.Join(t.TempDir(), "ledger.db")))
	if err != nil {
		t.Fatal(err)
	}
	h.SetMaxOpenConns(1)
	t.Cleanup(func() { h.Close() })
	if _, err = h.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	s := NewStore(h)
	a, _, err := s.CreateAccount(context.Background(), "synthetic-admin", "synthetic-user", "Synthetic account", "synthetic-password", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	p := Plan{ID: "lab", Version: 1, Name: "Synthetic lab", ValidSeconds: 86400, Start: "first-login", MaxSessions: 2, Networks: []string{"C"}, Down: 8000000, Up: 2000000, Speeds: []Speed{{ID: "fast", Name: "Fast", Down: 8000000, Up: 2000000}}, Policies: []Policy{{ID: "test", Limits: []Limit{{ID: "time", Kind: "seconds", State: "limited", Amount: 3600, Action: "disconnect", Period: Period{Kind: "daily", Zone: "Asia/Aden", Anchor: now.Add(-24 * time.Hour)}}}}}}
	if err = s.SavePlan(context.Background(), "synthetic-admin", "test", p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Assign(context.Background(), "synthetic-admin", a.ID, p.ID, p.Version, "test", now); err != nil {
		t.Fatal(err)
	}
	service := &RadiusService{Store: s, VerifySession: func(context.Context, Enforcement) bool { return true }, Mode: "pilot", Clients: []NAS{{RouterID: "lab", Network: "C", Address: "127.0.0.1", CalledStationID: "hs-lab", Secret: testSecret, DisconnectPort: 3799}}, PilotAccounts: map[string]bool{a.ID: true}}
	for _, configureService := range configure {
		configureService(service)
	}
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &radius.PacketServer{Handler: service, SecretSource: service, ErrorLog: log.New(io.Discard, "", 0)}
	go server.Serve(conn)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		server.Shutdown(ctx)
	})
	return s, service, a, conn.LocalAddr().String()
}
func access(password string) *radius.Packet {
	p := radius.New(radius.CodeAccessRequest, []byte(testSecret))
	rfc2865.UserName_SetString(p, "synthetic-user")
	rfc2865.UserPassword_SetString(p, password)
	rfc2865.CallingStationID_SetString(p, "02:00:00:00:00:01")
	rfc2865.CalledStationID_SetString(p, "hs-lab")
	signResponse(p)
	return p
}
func exchange(t *testing.T, p *radius.Packet, addr string) *radius.Packet {
	t.Helper()
	// This checks protocol correctness, not a latency SLA. Scrypt under -race
	// needs more than one second on the test host; still bound a stuck server.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := radius.Exchange(ctx, p, addr)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestRealUDPAccessAccountingAndReplay(t *testing.T) {
	s, _, a, addr := protocolFixture(t)
	p := access("synthetic-password")
	out := exchange(t, p, addr)
	if out.Code != radius.CodeAccessAccept {
		t.Fatal(out.Code)
	}
	lease := rfc2865.Class_GetString(out)
	if lease == "" || rfc2865.SessionTimeout_Get(out) != 60 || rfc2869.AcctInterimInterval_Get(out) != 60 || mikrotik.MikrotikRateLimit_GetString(out) != "2000000/8000000" {
		t.Fatal("invalid delegated attributes")
	}
	again := exchange(t, p, addr)
	if rfc2865.Class_GetString(again) != lease {
		t.Fatal("retry allocated twice")
	}
	acct := radius.New(radius.CodeAccountingRequest, []byte(testSecret))
	rfc2865.Class_SetString(acct, lease)
	rfc2865.CallingStationID_SetString(acct, "02:00:00:00:00:01")
	rfc2866.AcctSessionID_SetString(acct, "synthetic-session")
	rfc2866.AcctStatusType_Set(acct, rfc2866.AcctStatusType_Value_Stop)
	rfc2866.AcctInputOctets_Set(acct, 100)
	rfc2869.AcctInputGigawords_Set(acct, 1)
	rfc2866.AcctOutputOctets_Set(acct, 200)
	rfc2866.AcctSessionTime_Set(acct, 0)
	for i := 0; i < 2; i++ {
		if exchange(t, acct, addr).Code != radius.CodeAccountingResponse {
			t.Fatal("missing commit response")
		}
	}
	v, err := s.Snapshot(context.Background(), a.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if v.LifetimeUpload != 1<<32+100 || v.LifetimeDownload != 200 || len(v.Sessions) != 1 || v.Sessions[0].State != "stopped" {
		t.Fatal(v)
	}
}
func TestUnauthenticatedAccessAndForgedAccountingReceiveNoReply(t *testing.T) {
	s, _, _, addr := protocolFixture(t)
	for _, p := range []*radius.Packet{access("synthetic-password"), radius.New(radius.CodeAccountingRequest, []byte("wrong-synthetic-secret"))} {
		if p.Code == radius.CodeAccessRequest {
			rfc2869.MessageAuthenticator_Del(p)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		out, err := radius.Exchange(ctx, p, addr)
		cancel()
		if err == nil || out != nil {
			t.Fatal("forged packet accepted")
		}
	}
	var receipts int
	if err := s.sql.QueryRow(`SELECT count(*) FROM subscriber_accounting_receipts`).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatal("forged accounting reached durable storage", receipts, err)
	}

}
func TestOneUseTicketDeviceBindingAndRetransmission(t *testing.T) {
	s, _, a, addr := protocolFixture(t)
	token, err := s.IssueTicket(context.Background(), a.ID, "C", "02:00:00:00:00:01", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p := access(token)
	first := exchange(t, p, addr)
	if first.Code != radius.CodeAccessAccept {
		t.Fatal("valid ticket rejected")
	}
	if exchange(t, p, addr).Code != radius.CodeAccessAccept {
		t.Fatal("exact retransmission rejected")
	}
	if exchange(t, access(token), addr).Code != radius.CodeAccessReject {
		t.Fatal("ticket replay accepted")
	}
}
func TestNoAccountingResponseAfterStorageFailure(t *testing.T) {
	s, _, _, addr := protocolFixture(t)
	out := exchange(t, access("synthetic-password"), addr)
	s.sql.Close()
	p := radius.New(radius.CodeAccountingRequest, []byte(testSecret))
	rfc2865.Class_SetString(p, rfc2865.Class_GetString(out))
	rfc2866.AcctSessionID_SetString(p, "test")
	rfc2866.AcctStatusType_Set(p, rfc2866.AcctStatusType_Value_Start)
	rfc2865.CallingStationID_SetString(p, "02:00:00:00:00:01")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if out, err := radius.Exchange(ctx, p, addr); err == nil || out != nil {
		t.Fatal("acknowledged without durable commit")
	}
}
func TestGuardedBackgroundDisconnectWithoutBrowsers(t *testing.T) {
	s, service, a, _ := protocolFixture(t)
	now := time.Now().Add(-61 * time.Second)
	lease, err := s.Reserve(context.Background(), a.ID, "lab", "C", "device", "background-test", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Account(context.Background(), Accounting{LeaseID: lease.LeaseID, RouterID: "lab", Network: "C", Device: "device", SessionID: "session", Status: "start", At: now}); err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan struct{}, 1)
	nas := &radius.PacketServer{SecretSource: radius.StaticSecretSource([]byte(testSecret)), Handler: radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		if r.Packet.Code == radius.CodeDisconnectRequest {
			select {
			case received <- struct{}{}:
			default:
			}
			w.Write(r.Response(radius.CodeDisconnectACK))
		}
	})}
	go nas.Serve(conn)
	defer nas.Shutdown(context.Background())
	service.Clients[0].DisconnectPort = conn.LocalAddr().(*net.UDPAddr).Port
	service.Observe = func(context.Context, string) (RouterObservation, error) {
		at := time.Now()
		return RouterObservation{RouterID: "lab", At: at, Complete: true, Sessions: []ObservedSession{{ID: "*synthetic", Server: "hs-lab", Username: a.Username, Device: "device", Address: "198.51.100.2", Seconds: Quantity(at.Sub(now) / time.Second)}}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); service.Run(ctx, true, func(err error) { t.Log(err) }) }()
	select {
	case <-received:
	case <-time.After(4 * time.Second):
		t.Fatal("no background disconnect")
	}
	cancel()
	<-done
	due, err := s.Due(context.Background(), time.Now())
	if err != nil || len(due) != 1 {
		t.Fatal("missing Stop silently released its balance", due, err)
	}
}

func BenchmarkDurableDuplicateAccounting(b *testing.B) {
	h, err := sql.Open("sqlite", protocolFixtureURI(filepath.Join(b.TempDir(), "bench.db")))
	if err != nil {
		b.Fatal(err)
	}
	defer h.Close()
	h.SetMaxOpenConns(1)
	if _, err = h.Exec(Schema); err != nil {
		b.Fatal(err)
	}
	s := NewStore(h)
	ctx := context.Background()
	a, _, err := s.CreateAccount(ctx, "test", "benchmark", "Synthetic benchmark", "synthetic-password", "benchmark", 0)
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now()
	p := Plan{ID: "benchmark", Version: 1, Name: "Synthetic benchmark", ValidSeconds: 86400, Start: "first-login", MaxSessions: 1, Networks: []string{"C"}, Down: 8000000, Up: 2000000, Speeds: []Speed{{ID: "test", Name: "Test", Down: 8000000, Up: 2000000}}, Policies: []Policy{{ID: "p", Limits: []Limit{{ID: "t", Kind: "seconds", State: "limited", Amount: 3600, Period: Period{Kind: "daily", Zone: "Asia/Aden", Anchor: now.Add(-24 * time.Hour)}, Action: "disconnect"}}}}}
	if err = s.SavePlan(ctx, "test", "benchmark", p); err != nil {
		b.Fatal(err)
	}
	if _, err = s.Assign(ctx, "test", a.ID, p.ID, 1, "benchmark", now); err != nil {
		b.Fatal(err)
	}
	lease, err := s.Reserve(ctx, a.ID, "router", "C", "device", "request", now)
	if err != nil {
		b.Fatal(err)
	}
	e := Accounting{LeaseID: lease.LeaseID, RouterID: "router", Network: "C", Device: "device", SessionID: "session", Status: "start", At: now}
	if err = s.Account(ctx, e); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err = s.Account(ctx, e); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRADIUSAndPortalAuthenticationShareBoundedWork(t *testing.T) {
	s, service, _, addr := protocolFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	held, err := s.sql.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := radius.Exchange(ctx, access("synthetic-password"), addr); done <- err }()
	}
	// Holding the sole SQL connection makes the two real UDP handlers remain
	// inside credential verification without using timing assumptions about scrypt.
	deadline := time.Now().Add(time.Second)
	for {
		service.mu.Lock()
		count := len(service.authSlots)
		service.mu.Unlock()
		if count == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("RADIUS did not acquire the shared authentication guard")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := service.Authenticate(ctx, "synthetic-user", "synthetic-password"); err != ErrAuthenticationBusy {
		t.Fatal("portal credential path bypassed the shared bound", err)
	}
	if response := exchange(t, access("synthetic-password"), addr); response.Code != radius.CodeAccessReject {
		t.Fatal("RADIUS burst was not bounded")
	}
	held.Close()
	for i := 0; i < 2; i++ {
		if err = <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestSameNASNetworksAreResolvedFromAuthenticatedPacket(t *testing.T) {
	s, _, a, addr := protocolFixture(t, func(service *RadiusService) {
		n := service.Clients[0]
		n.Network = "D"
		n.CalledStationID = "hs-second"
		service.Clients = append(service.Clients, n)
	})
	plans, err := s.Plans(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := plans[0]
	p.Version++
	p.Networks = []string{"C", "D"}
	if err = s.SavePlan(t.Context(), "synthetic-admin", "same NAS lab", p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Assign(t.Context(), "synthetic-admin", a.ID, p.ID, p.Version, "same NAS lab", time.Now()); err != nil {
		t.Fatal(err)
	}
	req := access("synthetic-password")
	rfc2865.CalledStationID_SetString(req, "hs-second")
	signResponse(req)
	reply := exchange(t, req, addr)
	if reply.Code != radius.CodeAccessAccept {
		t.Fatal(reply.Code)
	}
	lease := rfc2865.Class_GetString(reply)
	var network string
	if err = s.sql.QueryRow(`SELECT network FROM subscriber_leases WHERE id=?`, lease).Scan(&network); err != nil || network != "D" {
		t.Fatal(network, err)
	}
	acct := radius.New(radius.CodeAccountingRequest, []byte(testSecret))
	rfc2865.Class_SetString(acct, lease)
	rfc2865.CallingStationID_SetString(acct, "02:00:00:00:00:01")
	rfc2866.AcctSessionID_SetString(acct, "same-nas-session")
	rfc2866.AcctStatusType_Set(acct, rfc2866.AcctStatusType_Value_Start)
	if exchange(t, acct, addr).Code != radius.CodeAccountingResponse {
		t.Fatal("Class-owned accounting rejected")
	}
	rfc2865.CalledStationID_SetString(acct, "hs-lab")
	ctx, cancel := context.WithTimeout(t.Context(), 80*time.Millisecond)
	defer cancel()
	if out, err := radius.Exchange(ctx, acct, addr); err == nil || out != nil {
		t.Fatal("forged network accounting accepted")
	}
}
