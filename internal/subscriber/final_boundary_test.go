package subscriber

import (
	"testing"
	"time"

	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

func TestMissingFinalReceiptCountDoesNotDoubleCountAbsentSession(t *testing.T) {
	s, svc, account, grant, start := liveFixture(t)
	at := start.Add(61 * time.Second)
	if err := s.ObserveRouter(t.Context(), RouterObservation{RouterID: "lab", At: at, Complete: true}, svc.Clients); err != nil {
		t.Fatal(err)
	}
	packet := receiptPacket(grant.LeaseID, "session", rfc2866.AcctStatusType_Value_Stop, nil, receiptUint32(10), receiptUint32(100))
	if err := rfc2865.CallingStationID_SetString(packet, "device"); err != nil {
		t.Fatal(err)
	}
	partial := accountingReceipt(packet, svc.Clients[0], at)
	if ack, err := s.ReceiveAccounting(t.Context(), partial); err != nil || !ack {
		t.Fatal(ack, err)
	}
	view, err := s.Snapshot(t.Context(), account.ID, at)
	if err != nil || view.PendingAccounting != 1 || view.Decision.Allowed {
		t.Fatal("one missing finality was counted twice or admitted", view, err)
	}
	packet = receiptPacket(grant.LeaseID, "session", rfc2866.AcctStatusType_Value_Stop, receiptUint32(20), receiptUint32(10), receiptUint32(100))
	if err := rfc2865.CallingStationID_SetString(packet, "device"); err != nil {
		t.Fatal(err)
	}
	final := accountingReceipt(packet, svc.Clients[0], at)
	if ack, err := s.ReceiveAccounting(t.Context(), final); err != nil || !ack {
		t.Fatal(ack, err)
	}
	view, err = s.Snapshot(t.Context(), account.ID, at)
	if err != nil || view.PendingAccounting != 0 || !view.Decision.Allowed || view.LifetimeSeconds != 20 {
		t.Fatal("covering final receipt did not settle both representations", view, err)
	}
}

func TestFinalStopDoesNotConflateBoundaryAllocationWithMissingFinality(t *testing.T) {
	s, svc, account, _ := protocolFixture(t)
	start := time.Now().UTC().Truncate(24 * time.Hour).Add(24*time.Hour - 10*time.Second)
	plans, err := s.Plans(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := plans[0]
	p.Version++
	period := Period{Kind: "daily", Zone: "UTC", Anchor: start.Add(10*time.Second - 24*time.Hour)}
	p.Policies[0].Limits[0].Period = period
	p.Policies[0].Limits = append(p.Policies[0].Limits, Limit{ID: "bytes", Kind: "download", State: "limited", Amount: 64 << 20, Action: "disconnect", Period: period})
	if err = s.SavePlan(t.Context(), "synthetic-admin", "boundary finality fixture", p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Assign(t.Context(), "synthetic-admin", account.ID, p.ID, p.Version, "boundary finality fixture", start); err != nil {
		t.Fatal(err)
	}
	grant, err := s.Reserve(t.Context(), account.ID, "lab", "C", "device", "boundary-finality", start)
	if err != nil {
		t.Fatal(err)
	}
	base := Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: "device", SessionID: "boundary", Status: "start", At: start}
	if err = s.Account(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	at := start.Add(61 * time.Second)
	if err = s.ObserveRouter(t.Context(), RouterObservation{RouterID: "lab", At: at, Complete: true}, svc.Clients); err != nil {
		t.Fatal(err)
	}
	base.Status, base.Seconds, base.Download, base.At = "stop", 20, 100, start.Add(20*time.Second)
	if err = s.Account(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	view, err := s.Snapshot(t.Context(), account.ID, at)
	if err != nil || view.PendingAccounting != 0 || !view.Decision.Allowed || view.LifetimeDownload != 100 {
		t.Fatal("a complete final reading was blocked as missing Stop", view, err)
	}
	gaps, err := s.ByteGaps(t.Context(), account.ID)
	if err != nil || len(gaps) != 1 || !gaps[0].ResolvedAt.IsZero() || view.Decision.Limits[1].Provisional != 100 {
		t.Fatal("period allocation disappeared or was falsely measured", gaps, view, err)
	}
}
