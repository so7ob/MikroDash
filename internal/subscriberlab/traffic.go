package subscriberlab

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/vendors/mikrotik"
	"mikrodash/internal/routeros"
)

// RuntimeTrafficProbe leaves the real MikroDash RADIUS/observer running. It
// serves only bounded synthetic payloads and does not create another ledger.
func (c *Client) RuntimeTrafficProbe(ctx context.Context, out io.Writer) error {
	rows, err := c.Do(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=.proplist=.id,user,server,mac-address,address"}, Timeout: 3 * time.Second})
	if err != nil || len(rows) != 1 || rows[0][".id"] == "" || rows[0]["user"] != "chr-lab" || rows[0]["server"] != "hs-lab" || rows[0]["mac-address"] != "02:00:00:00:54:02" || rows[0]["address"] != "198.51.100.2" {
		return errors.New("exact owned runtime client required for traffic probe")
	}
	record := &recorder{w: out}
	if err := c.trafficProbe(ctx, record, 65536); err != nil {
		return err
	}
	return record.Err()
}

func (c *Client) trafficProbe(ctx context.Context, record *recorder, downloadSize int) error {
	listener, err := net.Listen("tcp", "127.0.0.1:18080")
	if err != nil {
		return err
	}
	server := &http.Server{ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && r.URL.Path == "/download":
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Write(bytes.Repeat([]byte{'x'}, downloadSize))
			case r.Method == "POST" && r.URL.Path == "/upload":
				n, err := io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 16384))
				if err != nil || n != 16384 {
					http.Error(w, "invalid synthetic payload", 400)
					return
				}
				w.WriteHeader(204)
			default:
				http.NotFound(w, r)
			}
		})}
	go server.Serve(listener)
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		server.Shutdown(stop)
	}()
	read := func() (map[string]int64, error) {
		rows, err := c.Do(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=.proplist=user,bytes-in,bytes-out"}, Timeout: 5 * time.Second})
		if err != nil || len(rows) != 1 || rows[0]["user"] != "chr-lab" {
			return nil, errors.New("unique synthetic traffic session required")
		}
		result := map[string]int64{}
		for _, key := range []string{"bytes-in", "bytes-out"} {
			v, err := strconv.ParseInt(rows[0][key], 10, 64)
			if err != nil || v < 0 {
				return nil, errors.New("missing traffic counter")
			}
			result[key] = v
		}
		return result, nil
	}
	for _, direction := range []string{"download", "upload"} {
		before, err := read()
		if err != nil {
			return err
		}
		args := []string{"netns", "exec", "md-subscriber-lab", "curl", "--noproxy", "*", "--max-time", "30", "--silent", "--show-error", "--fail", "--output", "/dev/null"}
		if direction == "upload" {
			args = append(args, "--data-binary", "@-")
		}
		args = append(args, "http://10.0.2.2:18080/"+direction)
		cmd := exec.CommandContext(ctx, "ip", args...)
		if direction == "upload" {
			cmd.Stdin = bytes.NewReader(bytes.Repeat([]byte{'u'}, 16384))
		}
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		started := time.Now()
		if err := cmd.Run(); err != nil {
			return errors.New("isolated one-way traffic failed")
		}
		after, err := read()
		if err != nil {
			return err
		}
		deltaIn, deltaOut := after["bytes-in"]-before["bytes-in"], after["bytes-out"]-before["bytes-out"]
		record.emit("one-way-"+direction, map[string]string{"bytesInDelta": strconv.FormatInt(deltaIn, 10), "bytesOutDelta": strconv.FormatInt(deltaOut, 10), "clientTransferElapsed": time.Since(started).String()})
		if direction == "download" && (deltaOut < int64(downloadSize) || deltaOut <= deltaIn*4) || direction == "upload" && (deltaIn < 16384 || deltaIn <= deltaOut*4) {
			return errors.New("HotSpot traffic direction did not match input-upload")
		}
	}
	return nil
}

func (c *Client) rateProbe(ctx context.Context, secret, session string, up, down uint32, record *recorder) error {
	if _, err := c.Identity(); err != nil {
		return err
	}
	p := radius.New(radius.CodeCoARequest, []byte(secret))
	rate := strconv.FormatUint(uint64(up), 10) + "/" + strconv.FormatUint(uint64(down), 10)
	for _, err := range []error{
		rfc2865.UserName_SetString(p, "chr-lab"), rfc2866.AcctSessionID_SetString(p, session),
		rfc2865.FramedIPAddress_Set(p, net.IPv4(198, 51, 100, 2)),
		mikrotik.MikrotikRateLimit_SetString(p, rate),
	} {
		if err != nil {
			return err
		}
	}
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	reply, err := (&radius.Client{MaxPacketErrors: 10}).Exchange(deadline, p, "127.0.0.1:13799")
	if err != nil || reply.Code != radius.CodeCoAACK {
		return errors.New("real CHR rate CoA was not acknowledged")
	}
	queues, err := c.Do(routeros.Cmd{Path: "/queue/simple/print", Args: []string{"=.proplist=target,max-limit,limit-at,dynamic,disabled,invalid,parent,bucket-size,queue,rate,bytes,packets,dropped"}, Timeout: 5 * time.Second})
	if err != nil {
		return err
	}
	record.emit("rate-readback", queues)
	matching := 0
	for _, q := range queues {
		if q["target"] == "198.51.100.2/32" && q["dynamic"] == "true" && q["disabled"] == "false" && q["invalid"] != "true" && q["max-limit"] == rate {
			matching++
		}
	}
	if matching != 1 {
		return errors.New("rate ACK did not match one exact-device dynamic queue")
	}
	return nil
}
