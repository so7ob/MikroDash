package subscriber

import (
	"encoding/hex"
	"testing"
	"time"

	"layeh.com/radius/rfc2865"
)

func TestCoARequiresConfirmedHotSpotAddressAndFencesReplay(t *testing.T) {
	n := NAS{Secret: testSecret, Capabilities: recordedCapabilities()}
	o := Operation{Kind: "rate", Request: OperationRequest{Username: "synthetic", Device: "02:00:00:00:00:01", SessionID: "synthetic-session"}}
	o.Request.Admission.Decision.Down, o.Request.Admission.Decision.Up = 8000000, 2000000
	for _, address := range []string{"", "unknown", "0.0.0.0", "127.0.0.1", "224.0.0.1", "255.255.255.255", "2001:db8::2"} {
		if _, err := operationPacket(o, n, time.Now(), address); err == nil {
			t.Fatalf("unconfirmed/invalid address accepted: %q", address)
		}
	}
	p, err := operationPacket(o, n, time.Now(), "198.51.100.2")
	if err != nil || rfc2865.FramedIPAddress_Get(p).String() != "198.51.100.2" {
		t.Fatal("confirmed selector missing", err)
	}
	raw, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	o.Packet = hex.EncodeToString(raw)
	if _, err = operationPacket(o, n, time.Now(), "198.51.100.2"); err != nil {
		t.Fatal("exact owned retry refused", err)
	}
	if _, err = operationPacket(o, n, time.Now(), "198.51.100.3"); err == nil {
		t.Fatal("persisted packet replayed to a changed address")
	}
	rfc2865.FramedIPAddress_Del(p)
	raw, err = p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	o.Packet = hex.EncodeToString(raw)
	if _, err = operationPacket(o, n, time.Now(), "198.51.100.2"); err == nil {
		t.Fatal("legacy packet without a confirmed address replayed")
	}
}

func TestIssuedCoACannotVerifyAgainstChangedObservedAddress(t *testing.T) {
	s, svc, account, grant, start := liveFixture(t)
	o, err := s.PrepareOperation(t.Context(), grant.LeaseID, "rate", start.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	p, err := operationPacket(o, svc.Clients[0], start, "198.51.100.2")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.transitionOperation(t.Context(), o, "applying", "synthetic issued packet", hex.EncodeToString(raw), start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	at := start.Add(2 * time.Second)
	view := observation(svc, account, grant, start, at)
	view.Sessions[0].Address = "198.51.100.3"
	if err = s.ObserveRouter(t.Context(), view, svc.Clients); err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot(t.Context(), account.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ProcessOperations(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	operations, err := s.Operations(t.Context(), false)
	if err != nil || len(operations) != 1 || operations[0].State != "pending-reconnect" {
		t.Fatal("changed-address readback verified or replayed issued intent", operations, err)
	}
	after, err := s.Snapshot(t.Context(), account.ID, at)
	if err != nil || before.LifetimeSeconds != after.LifetimeSeconds || before.LifetimeDownload != after.LifetimeDownload || before.Subscription.ID != after.Subscription.ID || before.Subscription.ExpiresAt != after.Subscription.ExpiresAt {
		t.Fatal("selector rejection changed entitlement", err)
	}
	for i, limit := range before.Decision.Limits {
		if after.Decision.Limits[i].Reserved != limit.Reserved {
			t.Fatal("selector rejection released reserved credit")
		}
	}
}
