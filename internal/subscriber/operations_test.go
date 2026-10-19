package subscriber

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/vendors/mikrotik"
)

func recordedCapabilities() Capabilities {
	return Capabilities{Version: "synthetic-7", Evidence: "synthetic protocol lab only", TimeoutMode: "total", ByteMode: "total", CoARate: true, CoAGigawords: true, CoATotal: true, CounterMode: "input-upload"}
}
func liveFixture(t *testing.T, configure ...func(*Plan)) (*Store, *RadiusService, Account, Admission, time.Time) {
	t.Helper()
	s, svc, a, _ := protocolFixture(t)
	{
		plans, err := s.Plans(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		p := plans[0]
		p.Version++
		p.Policies[0].Limits = append(p.Policies[0].Limits, Limit{ID: "hard-data", Kind: "download", State: "limited", Amount: 64 << 20, Action: "disconnect", Period: p.Policies[0].Limits[0].Period})
		for _, change := range configure {
			change(&p)
		}
		if err = s.SavePlan(t.Context(), "synthetic-admin", "session lab", p); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Assign(t.Context(), "synthetic-admin", a.ID, p.ID, p.Version, "session lab", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().Truncate(time.Second)
	svc.Clients[0].Capabilities = recordedCapabilities()
	svc.AllowCoA = true
	admission, err := s.Reserve(t.Context(), a.ID, "lab", "C", "device", "operation-test", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Account(t.Context(), Accounting{LeaseID: admission.LeaseID, RouterID: "lab", Network: "C", Device: "device", SessionID: "session", Status: "start", At: now}); err != nil {
		t.Fatal(err)
	}
	return s, svc, a, admission, now
}
func observation(svc *RadiusService, a Account, grant Admission, start, now time.Time) RouterObservation {
	return RouterObservation{RouterID: "lab", Version: "synthetic-7", At: now, Complete: true, Sessions: []ObservedSession{{ID: "*synthetic", Username: a.Username, Device: "device", Server: "hs-lab", Address: "198.51.100.2", Seconds: Quantity(now.Sub(start) / time.Second), Download: Quantity(now.Sub(start)/time.Second) * 1000, CountersKnown: true, TimeLeft: grant.Decision.Timeout - int64(now.Sub(start)/time.Second), TimeoutKnown: true, DownloadLimit: grant.Download, UploadLimit: grant.Upload, TotalLimit: grant.Total, LimitsKnown: true, Up: grant.Decision.Up, Down: grant.Decision.Down, RateKnown: true}}}
}
func TestFiveContinuousRenewalsUseOneLedgerAndVerifiedReadback(t *testing.T) {
	s, svc, a, grant, start := liveFixture(t)
	now := start
	localEnd := start.Add(60 * time.Second)
	localDown, localUp := grant.Download, grant.Upload
	applied := 0
	var mu sync.Mutex
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	nas := &radius.PacketServer{SecretSource: radius.StaticSecretSource([]byte(testSecret)), Handler: radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Packet.Code != radius.CodeCoARequest || rfc2866.AcctSessionID_GetString(r.Packet) != "session" {
			t.Error("wrong session change")
			return
		}
		localEnd = start.Add(time.Duration(rfc2865.SessionTimeout_Get(r.Packet)) * time.Second)
		localDown = Quantity(mikrotik.MikrotikXmitLimit_Get(r.Packet))
		localUp = Quantity(mikrotik.MikrotikRecvLimit_Get(r.Packet))
		applied++
		_ = w.Write(r.Response(radius.CodeCoAACK))
	})}
	go nas.Serve(conn)
	defer nas.Shutdown(context.Background())
	svc.Clients[0].DisconnectPort = conn.LocalAddr().(*net.UDPAddr).Port
	svc.Observe = func(context.Context, string) (RouterObservation, error) {
		mu.Lock()
		defer mu.Unlock()
		v := observation(svc, a, grant, start, now)
		v.Sessions[0].TimeLeft = int64(localEnd.Sub(now) / time.Second)
		v.Sessions[0].DownloadLimit = localDown
		v.Sessions[0].UploadLimit = localUp
		return v, nil
	}
	for cycle := 1; cycle <= 5; cycle++ {
		now = start.Add(time.Duration(cycle*40) * time.Second)
		mu.Lock()
		expiry := localEnd
		mu.Unlock()
		if !expiry.After(now) {
			t.Fatal("continuous session expired")
		}
		if cycle == 3 {
			if _, err = s.sql.Exec(`CREATE TRIGGER synthetic_crash BEFORE UPDATE ON subscriber_operations WHEN NEW.state='acknowledged' BEGIN SELECT RAISE(ABORT,'synthetic crash after NAS apply'); END`); err != nil {
				t.Fatal(err)
			}
		}
		err = svc.Tick(t.Context(), now)
		if cycle == 3 {
			if err == nil {
				t.Fatal("fault injection was not reached")
			}
			if _, err = s.sql.Exec(`DROP TRIGGER synthetic_crash`); err != nil {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		snap, err := s.Snapshot(t.Context(), a.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		expiry = localEnd
		mu.Unlock()
		if snap.Sessions[0].Deadline.Equal(expiry) {
			t.Fatal("ACK was treated as verified")
		}
		// Restore a new service instance at every cycle: persisted intent and readback
		// recover even when the ACK handler could not finish before a process exit.
		next := &RadiusService{Store: NewStore(s.sql), Mode: svc.Mode, Clients: svc.Clients, AllowCoA: true, Observe: svc.Observe}
		now = now.Add(2 * time.Second)
		if err = next.Tick(t.Context(), now); err != nil {
			t.Fatal(err)
		}
		snap, err = s.Snapshot(t.Context(), a.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		if !snap.Sessions[0].Deadline.Equal(expiry) || snap.LifetimeSeconds != Quantity(cycle*40+2) || snap.LifetimeDownload != Quantity(cycle*40+2)*1000 || len(snap.Sessions) != 1 {
			t.Fatal("renewal reset or duplicated usage", snap)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if applied != 5 {
		t.Fatal("unexpected CoA count", applied)
	}
	// A delayed RADIUS reading overlaps the API high-water sample. It adds nothing.
	e := Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: "device", SessionID: "session", Status: "interim", Seconds: 201, Download: 201000, At: start.Add(201 * time.Second)}
	if err = s.Account(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil || snap.LifetimeSeconds != 202 || snap.LifetimeDownload != 202000 {
		t.Fatal(snap, err)
	}
}
func TestAbsenceReleasesSeatButPreservesUnknownExposureUntilStop(t *testing.T) {
	s, svc, a, grant, start := liveFixture(t)
	v := RouterObservation{RouterID: "lab", Version: "synthetic-7", At: start.Add(61 * time.Second), Complete: false, Sessions: []ObservedSession{}}
	if err := s.ObserveRouter(t.Context(), v, svc.Clients); err == nil {
		t.Fatal("failed read accepted as absence")
	}
	before, err := s.Snapshot(t.Context(), a.ID, v.At)
	if err != nil || before.Sessions[0].State == "stopped" {
		t.Fatal(before, err)
	}
	v.Complete = true
	for range 2 {
		if err = s.ObserveRouter(t.Context(), v, svc.Clients); err != nil {
			t.Fatal(err)
		}
	}
	settled, err := s.Snapshot(t.Context(), a.ID, v.At)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Sessions[0].State != "stopped" || settled.LifetimeSeconds != 0 || settled.Decision.Limits[0].Reserved != 60 {
		t.Fatal("unknown usage became free or measured usage", settled)
	}
	final := Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: "device", SessionID: "session", Status: "stop", Seconds: 20, At: start.Add(20 * time.Second)}
	if err = s.Account(t.Context(), final); err != nil {
		t.Fatal(err)
	}
	settled, err = s.Snapshot(t.Context(), a.ID, v.At)
	if err != nil || settled.Decision.Limits[0].Reserved != 0 || settled.Decision.Limits[0].Remaining != 3580 {
		t.Fatal(settled, err)
	}
}
func TestRateOperationHasNoQuotaOrExpirySideEffects(t *testing.T) {
	s, svc, a, grant, start := liveFixture(t)
	before, err := s.Snapshot(t.Context(), a.ID, start)
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.PrepareOperation(t.Context(), grant.LeaseID, "rate", start.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.Snapshot(t.Context(), a.ID, start)
	if err != nil {
		t.Fatal(err)
	}
	if before.Version != after.Version {
		t.Fatal("preparing rate changed quota, expiry or identity")
	}
	bad := svc.Clients[0]
	bad.Capabilities = Capabilities{}
	if _, err = operationPacket(o, bad, start); err == nil {
		t.Fatal("unverified rate capability accepted")
	}
	if err = s.transitionOperation(t.Context(), o, "applying", "test", "", start); err != nil {
		t.Fatal(err)
	}
	row := observation(svc, a, grant, start, start.Add(2*time.Second)).Sessions[0]
	row.Down /= 2
	if operationVerified(o, row, start.Add(2*time.Second)) {
		t.Fatal("wrong applied rate verified")
	}
}
func TestRenewalDoesNotReleaseAnotherDevicesReservation(t *testing.T) {
	s, svc, a, grant, start := liveFixture(t)
	second, err := s.Reserve(t.Context(), a.ID, "lab", "C", "device-two", "second", start)
	if err != nil {
		t.Fatal(err)
	}
	if second.LeaseID == grant.LeaseID {
		t.Fatal("same lease")
	}
	v := observation(svc, a, grant, start, start.Add(40*time.Second))
	if err = s.ObserveRouter(t.Context(), v, svc.Clients); err != nil {
		t.Fatal(err)
	}
	o, err := s.PrepareOperation(t.Context(), grant.LeaseID, "renew", v.At)
	if err != nil {
		t.Fatal(err)
	}
	var total Quantity
	if err = s.sql.QueryRow(`SELECT sum(amount) FROM subscriber_reservations WHERE limit_id='time'`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 120 || o.Request.Admission.Decision.Timeout != 60 {
		t.Fatal("reservation granted twice or other lease ignored", total, o)
	}
	// Unknown/stale usage never authorizes another full delegation.
	if _, err = s.PrepareOperation(t.Context(), grant.LeaseID, "renew", v.At); err != nil {
		t.Fatal(err)
	}
}

func TestRealUDPRateSwitchAndActivePolicyThrottle(t *testing.T) {
	s, svc, a, grant, start := liveFixture(t, func(p *Plan) {
		p.Speeds = append([]Speed{{ID: "economy", Name: "Economy", Down: 2000000, Up: 1000000}}, p.Speeds...)
		period := p.Policies[0].Limits[0].Period
		p.Policies[0].Limits = append(p.Policies[0].Limits, Limit{ID: "soft-data", Kind: "download", State: "limited", Amount: 10000, Action: "throttle", Down: 1000000, Up: 500000, Period: period})
	})
	now := start
	up, down := grant.Decision.Up, grant.Decision.Down
	var mu sync.Mutex
	changes := 0
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	nas := &radius.PacketServer{SecretSource: radius.StaticSecretSource([]byte(testSecret)), Handler: radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		mu.Lock()
		defer mu.Unlock()
		if rfc2866.AcctSessionID_GetString(r.Packet) != "session" || rfc2865.CallingStationID_GetString(r.Packet) != "device" {
			t.Error("wrong device")
			return
		}
		// Decode through the actual RADIUS vendor attribute, not a store callback.
		if _, err := fmt.Sscanf(mikrotik.MikrotikRateLimit_GetString(r.Packet), "%d/%d", &up, &down); err != nil {
			t.Error(err)
			return
		}
		changes++
		_ = w.Write(r.Response(radius.CodeCoAACK))
	})}
	go nas.Serve(conn)
	defer nas.Shutdown(context.Background())
	svc.Clients[0].DisconnectPort = conn.LocalAddr().(*net.UDPAddr).Port
	svc.Observe = func(context.Context, string) (RouterObservation, error) {
		mu.Lock()
		defer mu.Unlock()
		v := observation(svc, a, grant, start, now)
		v.Sessions[0].Up = up
		v.Sessions[0].Down = down
		return v, nil
	}
	now = start.Add(2 * time.Second)
	if err = svc.Tick(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ChooseSpeed(t.Context(), a.ID, "fast", now); err != nil {
		t.Fatal(err)
	}
	choice, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if choice.Subscription.SpeedState != "requested" || choice.Subscription.ID != before.Subscription.ID || !choice.Subscription.ExpiresAt.Equal(before.Subscription.ExpiresAt) || choice.LifetimeSeconds != before.LifetimeSeconds || choice.Decision.Limits[0].Reserved != before.Decision.Limits[0].Reserved {
		t.Fatal("choice changed entitlement", choice)
	}
	if err = svc.Tick(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err = svc.Tick(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	current, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if current.Subscription.SpeedState != "verified" || current.Sessions[0].AppliedDown != 8000000 || current.Sessions[0].AppliedUp != 2000000 {
		t.Fatal("live rate was not verified", current)
	}
	now = start.Add(20 * time.Second)
	if err = svc.Tick(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err = svc.Tick(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	current, err = s.Snapshot(t.Context(), a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if current.Sessions[0].AppliedDown != 1000000 || current.Sessions[0].AppliedUp != 500000 || current.LifetimeSeconds != 22 || current.Subscription.ID != before.Subscription.ID {
		t.Fatal("active throttle was not applied", current)
	}
	mu.Lock()
	defer mu.Unlock()
	if changes != 2 {
		t.Fatal("unexpected rate change count", changes)
	}
}

func TestCoATimeoutAndByteModeEncoding(t *testing.T) {
	start := time.Unix(100000, 0)
	now := start.Add(40 * time.Second)
	o := Operation{Kind: "renew", CreatedAt: now, TargetDeadline: start.Add(100 * time.Second), Request: OperationRequest{StartedAt: start, SessionID: "session", Device: "device", Username: "synthetic", DownloadCeiling: 300, UploadCeiling: 150, Admission: Admission{Download: 200, Upload: 100}}}
	for _, mode := range []string{"total", "remaining"} {
		n := NAS{Secret: testSecret, CalledStationID: "hs-lab", Capabilities: recordedCapabilities()}
		n.Capabilities.TimeoutMode = mode
		n.Capabilities.ByteMode = mode
		p, err := operationPacket(o, n, now)
		if mode == "remaining" {
			if err == nil {
				t.Fatal("relative CoA could regrant quota after an uncertain delayed retry")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		timeout := rfc2865.SessionTimeout_Get(p)
		down := mikrotik.MikrotikXmitLimit_Get(p)
		if mode == "total" && (timeout != 100 || down != 300) {
			t.Fatal(mode, timeout, down)
		}
		if mode == "remaining" && (timeout != 60 || down != 200) {
			t.Fatal(mode, timeout, down)
		}
		// A shortening request uses the same documented/probed mode explicitly.
		shorter := o
		shorter.TargetDeadline = start.Add(50 * time.Second)
		p, err = operationPacket(shorter, n, now)
		if err != nil {
			t.Fatal(err)
		}
		want := rfc2865.SessionTimeout(50)
		if mode == "remaining" {
			want = 10
		}
		if rfc2865.SessionTimeout_Get(p) != want {
			t.Fatal("shortened timeout encoded incorrectly", mode)
		}
	}
}

func TestSelectingAlreadyAppliedRateNeedsReadbackButNoCoA(t *testing.T) {
	s, svc, a, grant, start := liveFixture(t)
	now := start.Add(2 * time.Second)
	svc.Observe = func(context.Context, string) (RouterObservation, error) {
		return observation(svc, a, grant, start, now), nil
	}
	if err := s.ChooseSpeed(t.Context(), a.ID, "fast", now); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if err := svc.Tick(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	v, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil || v.Subscription.SpeedState != "verified" {
		t.Fatal(v, err)
	}
	ops, err := s.Operations(t.Context(), false)
	if err != nil || len(ops) != 1 || ops[0].Packet != "" {
		t.Fatal("redundant network write for an existing verified rate", ops, err)
	}
}

func TestPersistedCoARetryRequiresCurrentCapabilities(t *testing.T) {
	start := time.Unix(100000, 0)
	for _, kind := range []string{"renew", "rate"} {
		o := Operation{Kind: kind, TargetDeadline: start.Add(100 * time.Second), Request: OperationRequest{StartedAt: start, DownloadCeiling: 1 << 33, UploadCeiling: 100, TotalCeiling: 1 << 34}}
		n := NAS{Secret: testSecret, Capabilities: recordedCapabilities()}
		p, err := operationPacket(o, n, start)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := p.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		o.Packet = hex.EncodeToString(raw)
		mutations := map[string]func(*Capabilities){
			"removed evidence": func(c *Capabilities) { c.Evidence = "" },
		}
		if kind == "rate" {
			mutations["removed rate"] = func(c *Capabilities) { c.CoARate = false }
		} else {
			mutations["relative timeout"] = func(c *Capabilities) { c.TimeoutMode = "remaining" }
			mutations["relative bytes"] = func(c *Capabilities) { c.ByteMode = "remaining" }
			mutations["unknown direction"] = func(c *Capabilities) { c.CounterMode = "" }
			mutations["removed total"] = func(c *Capabilities) { c.CoATotal = false }
			mutations["removed gigawords"] = func(c *Capabilities) { c.CoAGigawords = false }
		}
		for name, mutate := range mutations {
			t.Run(kind+"/"+name, func(t *testing.T) {
				changed := n
				mutate(&changed.Capabilities)
				if _, err := operationPacket(o, changed, start); err == nil {
					t.Fatal("persisted packet bypassed revoked capability")
				}
			})
		}
	}
}
