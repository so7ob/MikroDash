package subscriber

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/vendors/mikrotik"
)

func TestCalendarRenewalAppliesFutureThrottleOverAuthenticatedUDP(t *testing.T) {
	c := resetCases(t)[0]
	s, svc, a, grant, start := crossingFixture(t, c.period, c.edge, func(p *Plan) {
		p.Policies[0].Limits = append(p.Policies[0].Limits, Limit{ID: "future-throttle", Kind: "download", State: "limited", Amount: 100000, Period: c.period, Action: "throttle", Down: 1000000, Up: 500000})
	})
	if err := s.Account(t.Context(), Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: "device", SessionID: "session", Status: "start", At: start}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot(t.Context(), a.ID, start)
	if err != nil {
		t.Fatal(err)
	}
	// A future period can already have an approved/imported opening balance.
	// The renewal must intersect that period before delegating through its edge.
	if _, err = s.sql.Exec(`INSERT INTO subscriber_buckets VALUES(?,?,?,?,?)`, before.Subscription.ID, "future-throttle", c.edge.Unix(), 100000, "approved-settlement"); err != nil {
		t.Fatal(err)
	}
	now := start.Add(5 * time.Second)
	if err = s.ObserveRouter(t.Context(), observation(svc, a, grant, start, now), svc.Clients); err != nil {
		t.Fatal(err)
	}
	op, err := s.PrepareOperation(t.Context(), grant.LeaseID, "renew", now)
	if err != nil || !op.Request.RequireRate || op.Request.Admission.Decision.Down != 1000000 || !op.TargetDeadline.After(c.edge) {
		t.Fatal("future throttle was omitted from the persisted grant", op, err)
	}
	// Revoking live-rate capability must refuse this combined grant before a
	// packet can leave, even though timeout and byte CoA capabilities remain.
	unverified := svc.Clients[0]
	unverified.Capabilities.CoARate = false
	if _, err = operationPacket(op, unverified, now, "198.51.100.2"); err == nil {
		t.Fatal("combined renewal bypassed live-rate capability")
	}
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var applied atomic.Int64
	nas := &radius.PacketServer{SecretSource: radius.StaticSecretSource([]byte(testSecret)), Handler: radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		if r.Code != radius.CodeCoARequest || rfc2866.AcctSessionID_GetString(r.Packet) != "session" || rfc2865.FramedIPAddress_Get(r.Packet).String() != "198.51.100.2" || rfc2865.CallingStationID_GetString(r.Packet) != "device" {
			t.Error("combined grant targeted a different session")
			return
		}
		if mikrotik.MikrotikRateLimit_GetString(r.Packet) != "500000/1000000" || int64(rfc2865.SessionTimeout_Get(r.Packet)) != op.TargetDeadline.Unix()-start.Unix() || Quantity(mikrotik.MikrotikXmitLimit_Get(r.Packet)) != op.Request.DownloadCeiling || Quantity(mikrotik.MikrotikRecvLimit_Get(r.Packet)) != op.Request.UploadCeiling {
			t.Error("wire grant differs from reserved timeout, directional byte caps or future throttle")
			return
		}
		applied.Add(1)
		_ = w.Write(r.Response(radius.CodeCoAACK))
	})}
	go nas.Serve(conn)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := nas.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	svc.Clients[0].DisconnectPort = conn.LocalAddr().(*net.UDPAddr).Port
	svc.AllowCoA = true
	if err = svc.ProcessOperations(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	operations, err := s.Operations(t.Context(), false)
	if err != nil || len(operations) != 1 || operations[0].State != "acknowledged" || applied.Load() != 1 {
		t.Fatal("combined UDP grant was not acknowledged", operations, applied.Load(), err)
	}
	for _, correctRate := range []bool{false, true} {
		now = now.Add(time.Second)
		sample := observation(svc, a, grant, start, now)
		row := &sample.Sessions[0]
		row.TimeLeft = op.TargetDeadline.Unix() - now.Unix()
		row.DownloadLimit = op.Request.DownloadCeiling
		row.UploadLimit = op.Request.UploadCeiling
		row.TotalLimit = op.Request.TotalCeiling
		if correctRate {
			row.Down = 1000000
			row.Up = 500000
		}
		if err = s.ObserveRouter(t.Context(), sample, svc.Clients); err != nil {
			t.Fatal(err)
		}
		if err = svc.ProcessOperations(t.Context(), now); err != nil {
			t.Fatal(err)
		}
		operations, err = s.Operations(t.Context(), false)
		if err != nil || len(operations) != 1 {
			t.Fatal(operations, err)
		}
		if correctRate && operations[0].State != "verified" {
			t.Fatal("exact combined readback did not verify", operations)
		}
		if !correctRate && operations[0].State != "acknowledged" {
			t.Fatal("timeout/bytes alone falsely verified the missing rate change", operations)
		}
	}
	after, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil || after.Subscription.ID != before.Subscription.ID || !after.Subscription.ExpiresAt.Equal(before.Subscription.ExpiresAt) || after.LifetimeSeconds != 7 || after.LifetimeDownload != 7000 || applied.Load() != 1 {
		t.Fatal("combined change reset entitlement or repeated application", after, applied.Load(), err)
	}
}
