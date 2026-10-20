package server

import (
	"context"
	"errors"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"mikrodash/internal/routeros"
	"mikrodash/internal/subscriber"
)

// subscriberObserveRouter uses the retained router session and its shared
// limiter. One active read and one queue read serve every subscriber on a NAS.
func (s *Server) subscriberObserveRouter(ctx context.Context, id string) (subscriber.RouterObservation, error) {
	out := subscriber.RouterObservation{RouterID: id, Sessions: []subscriber.ObservedSession{}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	rs := s.sessions.Live()[id]
	if rs == nil || !rs.Connected() {
		return out, errors.New("subscriber router is unavailable")
	}
	resource, err := rs.Exec(routeros.Cmd{Path: "/system/resource/print", Args: []string{"=.proplist=version"}, Timeout: 2 * time.Second})
	if err != nil || len(resource) != 1 {
		return out, errors.New("subscriber router version unavailable")
	}
	out.Version = resource[0]["version"]
	active, err := rs.Exec(routeros.Cmd{Path: "/ip/hotspot/active/print", Args: []string{"=stats=", "=.proplist=.id,user,mac-address,server,address,uptime,bytes-in,bytes-out,session-time-left,limit-bytes-in,limit-bytes-out,limit-bytes-total"}, Timeout: 2 * time.Second})
	if err != nil {
		return out, err
	}
	at := time.Now()
	queues, qerr := rs.Exec(routeros.Cmd{Path: "/queue/simple/print", Args: []string{"=.proplist=.id,name,target,max-limit,dynamic,disabled"}, Timeout: 2 * time.Second})
	for _, row := range active {
		if row[".id"] == "" || row["user"] == "" || row["mac-address"] == "" || row["server"] == "" || row["uptime"] == "" {
			return out, errors.New("incomplete active session identity")
		}
		n, found := subscriberNASForServer(s.subscribers.cfg.Clients, id, row["server"])
		if !found {
			continue
		}
		seconds, known := subscriberDuration(row["uptime"])
		if !known {
			return out, errors.New("invalid active session uptime")
		}
		v := subscriber.ObservedSession{ID: row[".id"], Server: row["server"], Username: row["user"], Device: row["mac-address"], Address: row["address"], Seconds: seconds}
		left, known := subscriberDuration(row["session-time-left"])
		v.TimeLeft, v.TimeoutKnown = int64(left), known
		input, knownIn := subscriberCounter(row["bytes-in"])
		output, knownOut := subscriberCounter(row["bytes-out"])
		v.CountersKnown = knownIn && knownOut
		switch n.Capabilities.CounterMode {
		case "input-upload":
			v.Upload = input
			v.Download = output
		case "output-upload":
			v.Upload = output
			v.Download = input
		default:
			v.CountersKnown = false
		}
		var inLimit, outLimit bool
		v.UploadLimit, inLimit = subscriberCounter(row["limit-bytes-in"])
		v.DownloadLimit, outLimit = subscriberCounter(row["limit-bytes-out"])
		v.TotalLimit, _ = subscriberCounter(row["limit-bytes-total"])
		v.LimitsKnown = inLimit && outLimit
		if qerr == nil {
			v.Up, v.Down, v.RateKnown = subscriberQueueRate(queues, row["address"])
		}
		out.Sessions = append(out.Sessions, v)
	}
	out.At = at
	out.Complete = true
	return out, nil
}
func subscriberNASForServer(clients []subscriber.NAS, router, server string) (subscriber.NAS, bool) {
	var result subscriber.NAS
	count := 0
	for _, n := range clients {
		if n.RouterID == router && n.CalledStationID == server {
			result = n
			count++
		}
	}
	return result, count == 1
}
func subscriberCounter(s string) (subscriber.Quantity, bool) {
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	return subscriber.Quantity(v), err == nil && v >= 0
}
func subscriberQueueRate(queues []routeros.Reply, address string) (subscriber.Quantity, subscriber.Quantity, bool) {
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return 0, 0, false
	}
	count := 0
	var up, down subscriber.Quantity
	for _, q := range queues {
		if q["dynamic"] != "true" || q["disabled"] == "true" {
			continue
		}
		// A multi-target or subnet-wide queue cannot prove a per-device rate.
		target := q["target"]
		if strings.Contains(target, ",") {
			continue
		}
		prefix, err := netip.ParsePrefix(target)
		if err != nil || prefix.Addr() != ip || prefix.Bits() != ip.BitLen() {
			continue
		}
		parts := strings.Split(q["max-limit"], "/")
		if len(parts) != 2 {
			continue
		}
		u, uk := subscriberCounter(parts[0])
		d, dk := subscriberCounter(parts[1])
		if !uk || !dk || u == 0 || d == 0 {
			continue
		}
		up, down = u, d
		count++
	}
	return up, down, count == 1
}
