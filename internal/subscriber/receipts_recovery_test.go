package subscriber

import (
	"database/sql"
	"strconv"
	"testing"
	"time"

	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
)

func TestReceiptCoverageUsesReliableAPIWithoutTreatingItAsFinalStop(t *testing.T) {
	s, _, a, _ := protocolFixture(t)
	start := time.Now().Truncate(time.Second)
	grant, err := s.Reserve(t.Context(), a.ID, "lab", "C", "02:00:00:00:00:01", "receipt-api", start)
	if err != nil {
		t.Fatal(err)
	}
	e := Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: "02:00:00:00:00:01", SessionID: "api-session", Status: "start", At: start}
	if err = s.Account(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"start", "stop"} {
		claim := e
		claim.Source = "api"
		claim.Status = status
		if err = s.Account(t.Context(), claim); err == nil {
			t.Fatal("an API snapshot claimed lifecycle authority", status)
		}
	}
	n := NAS{RouterID: "lab", Network: "C"}
	partial := accountingReceipt(receiptPacket(grant.LeaseID, e.SessionID, rfc2866.AcctStatusType_Value_InterimUpdate, receiptUint32(20), receiptUint32(50), nil), n, start.Add(20*time.Second))
	if ack, err := s.ReceiveAccounting(t.Context(), partial); err != nil || !ack {
		t.Fatal(ack, err)
	}
	e.Source = "api"
	e.Status = "interim"
	e.Seconds = 20
	e.Upload = 50
	e.Download = 100
	e.At = start.Add(19 * time.Second)
	if err = s.Account(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	v, err := s.Snapshot(t.Context(), a.ID, e.At)
	if err != nil || v.PendingAccounting != 1 {
		t.Fatal("older API read cleared a newer receipt", v, err)
	}
	e.At = start.Add(20 * time.Second)
	if err = s.Account(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	v, err = s.Snapshot(t.Context(), a.ID, e.At)
	if err != nil || v.PendingAccounting != 0 || v.LifetimeSeconds != 20 || v.LifetimeDownload != 100 {
		t.Fatal(v, err)
	}
	partial = accountingReceipt(receiptPacket(grant.LeaseID, e.SessionID, rfc2866.AcctStatusType_Value_Stop, nil, receiptUint32(50), receiptUint32(100)), n, start.Add(21*time.Second))
	if ack, err := s.ReceiveAccounting(t.Context(), partial); err != nil || !ack {
		t.Fatal(ack, err)
	}
	e.Seconds = 21
	e.At = start.Add(21 * time.Second)
	if err = s.Account(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	v, err = s.Snapshot(t.Context(), a.ID, e.At)
	if err != nil || v.PendingAccounting != 1 {
		t.Fatal("an API snapshot proved a final Stop", v, err)
	}
	if _, err = s.PrepareOperation(t.Context(), grant.LeaseID, "renew", e.At); err == nil {
		t.Fatal("renewed unresolved final accounting")
	}
	final := accountingReceipt(receiptPacket(grant.LeaseID, e.SessionID, rfc2866.AcctStatusType_Value_Stop, receiptUint32(21), receiptUint32(50), receiptUint32(100)), n, e.At)
	if ack, err := s.ReceiveAccounting(t.Context(), final); err != nil || !ack {
		t.Fatal(ack, err)
	}
	v, err = s.Snapshot(t.Context(), a.ID, e.At)
	if err != nil || v.PendingAccounting != 0 || v.LifetimeSeconds != 21 || v.LifetimeDownload != 100 || v.LifetimeUpload != 50 {
		t.Fatal("overlapping sources duplicated consumption", v, err)
	}
}

func TestReceiptLatePartialAfterFinalStopDoesNotLockTheAccount(t *testing.T) {
	s, _, a, _ := protocolFixture(t)
	start := time.Now().Truncate(time.Second)
	grant, err := s.Reserve(t.Context(), a.ID, "lab", "C", "02:00:00:00:00:01", "late-partial", start)
	if err != nil {
		t.Fatal(err)
	}
	n := NAS{RouterID: "lab", Network: "C"}
	final := accountingReceipt(receiptPacket(grant.LeaseID, "late-session", rfc2866.AcctStatusType_Value_Stop, receiptUint32(20), receiptUint32(50), receiptUint32(100)), n, start.Add(20*time.Second))
	if ack, err := s.ReceiveAccounting(t.Context(), final); err != nil || !ack {
		t.Fatal(ack, err)
	}
	partial := accountingReceipt(receiptPacket(grant.LeaseID, "late-session", rfc2866.AcctStatusType_Value_Stop, nil, receiptUint32(50), receiptUint32(100)), n, start.Add(21*time.Second))
	if ack, err := s.ReceiveAccounting(t.Context(), partial); err != nil || !ack {
		t.Fatal(ack, err)
	}
	v, err := s.Snapshot(t.Context(), a.ID, partial.At)
	if err != nil || v.PendingAccounting != 0 || v.LifetimeSeconds != 20 {
		t.Fatal("late covered evidence blocked a settled account", v, err)
	}
	conflict := accountingReceipt(receiptPacket(grant.LeaseID, "late-session", rfc2866.AcctStatusType_Value_Stop, receiptUint32(21), nil, receiptUint32(101)), n, start.Add(22*time.Second))
	if ack, err := s.ReceiveAccounting(t.Context(), conflict); err != nil || !ack {
		t.Fatal(ack, err)
	}
	in := FinalReading{LeaseID: grant.LeaseID, Seconds: 20, Download: 100, Upload: 50, Reason: "Synthetic final correction", Evidence: "Synthetic independent counters"}
	if err = s.ReconcileMissingStop(t.Context(), "synthetic-admin", in, conflict.At); err == nil {
		t.Fatal("approved counters below a recorded floor")
	}
	in.Download = 101
	for range 2 {
		if err = s.ReconcileMissingStop(t.Context(), "synthetic-admin", in, conflict.At); err != nil {
			t.Fatal(err)
		}
	}
	v, err = s.Snapshot(t.Context(), a.ID, conflict.At)
	if err != nil || v.PendingAccounting != 0 || v.LifetimeSeconds != 21 || v.LifetimeDownload != 101 {
		t.Fatal("one-second skew lost trustworthy counters", v, err)
	}
}

func TestReceiptPendingSurvivesSQLiteCloseAndReopen(t *testing.T) {
	s, _, a, _ := protocolFixture(t)
	start := time.Now().Truncate(time.Second)
	grant, err := s.Reserve(t.Context(), a.ID, "lab", "C", "02:00:00:00:00:01", "reopen", start)
	if err != nil {
		t.Fatal(err)
	}
	n := NAS{RouterID: "lab", Network: "C"}
	partial := accountingReceipt(receiptPacket(grant.LeaseID, "reopen-session", rfc2866.AcctStatusType_Value_Stop, nil, receiptUint32(50), receiptUint32(100)), n, start.Add(20*time.Second))
	if ack, err := s.ReceiveAccounting(t.Context(), partial); err != nil || !ack {
		t.Fatal(ack, err)
	}
	var seq int
	var name, path string
	if err = s.sql.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err = s.sql.Close(); err != nil {
		t.Fatal(err)
	}
	h, err := sql.Open("sqlite", protocolFixtureURI(path))
	if err != nil {
		t.Fatal(err)
	}
	h.SetMaxOpenConns(1)
	t.Cleanup(func() { h.Close() })
	reopened := NewStore(h)
	v, err := reopened.Snapshot(t.Context(), a.ID, partial.At)
	if err != nil || v.PendingAccounting != 1 || v.Decision.Allowed {
		t.Fatal("restart lost pending accounting", v, err)
	}
	final := accountingReceipt(receiptPacket(grant.LeaseID, "reopen-session", rfc2866.AcctStatusType_Value_Stop, receiptUint32(20), receiptUint32(50), receiptUint32(100)), n, partial.At)
	for range 2 {
		if ack, err := reopened.ReceiveAccounting(t.Context(), final); err != nil || !ack {
			t.Fatal(ack, err)
		}
	}
	v, err = reopened.Snapshot(t.Context(), a.ID, partial.At)
	if err != nil || v.PendingAccounting != 0 || v.LifetimeSeconds != 20 || v.LifetimeDownload != 100 {
		t.Fatal(v, err)
	}
}

func FuzzReceiptRangesAndRetryIdentity(f *testing.F) {
	for _, n := range []uint64{0, 4294967296, 9007199254740993, 9223372036854775807, 18446744073709551615} {
		f.Add(uint32(20), n, uint64(0), uint32(5))
	}
	f.Fuzz(func(t *testing.T, seconds uint32, up, down uint64, delay uint32) {
		p := receiptPacket("synthetic-lease", "session", rfc2866.AcctStatusType_Value_Stop, &seconds, receiptUint32(uint32(up)), receiptUint32(uint32(down)))
		_ = rfc2869.AcctInputGigawords_Set(p, rfc2869.AcctInputGigawords(up>>32))
		_ = rfc2869.AcctOutputGigawords_Set(p, rfc2869.AcctOutputGigawords(down>>32))
		n := NAS{RouterID: "lab", Network: "C"}
		now := time.Now()
		r := accountingReceipt(p, n, now)
		if r.Upload != strconv.FormatUint(up, 10) || r.Download != strconv.FormatUint(down, 10) {
			t.Fatal("unsigned counter lost precision", r)
		}
		e, err := r.reading()
		const max = uint64(1<<63 - 1)
		valid := seconds <= 315360000 && up <= max && down <= max && up <= max-down
		if valid != (err == nil) {
			t.Fatal("range decision", valid, e, err)
		}
		_ = rfc2866.AcctDelayTime_Set(p, rfc2866.AcctDelayTime(delay))
		p.Identifier++
		if retry := accountingReceipt(p, n, now.Add(time.Second)); retry.ID != r.ID {
			t.Fatal("transport retry changed financial identity")
		}
	})
}
