package subscriber

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
)

func receiptUint32(v uint32) *uint32 { return &v }
func receiptPacket(lease, session string, status rfc2866.AcctStatusType, seconds, input, output *uint32) *radius.Packet {
	p := radius.New(radius.CodeAccountingRequest, []byte(testSecret))
	_ = rfc2865.Class_SetString(p, lease)
	_ = rfc2865.CallingStationID_SetString(p, "02:00:00:00:00:01")
	_ = rfc2865.CalledStationID_SetString(p, "hs-lab")
	_ = rfc2866.AcctSessionID_SetString(p, session)
	_ = rfc2866.AcctStatusType_Set(p, status)
	if seconds != nil {
		_ = rfc2866.AcctSessionTime_Set(p, rfc2866.AcctSessionTime(*seconds))
	}
	if input != nil {
		_ = rfc2866.AcctInputOctets_Set(p, rfc2866.AcctInputOctets(*input))
	}
	if output != nil {
		_ = rfc2866.AcctOutputOctets_Set(p, rfc2866.AcctOutputOctets(*output))
	}
	return p
}

func TestReceiptParserDistinguishesMissingZeroMalformedAndLargeCounters(t *testing.T) {
	n := NAS{RouterID: "lab", Network: "C"}
	now := time.Now()
	p := receiptPacket("synthetic-lease", "session", rfc2866.AcctStatusType_Value_Stop, receiptUint32(0), nil, receiptUint32(0))
	missing := accountingReceipt(p, n, now)
	if missing.UploadKnown || !missing.DownloadKnown || missing.Upload != "" || missing.Download != "0" {
		t.Fatal(missing)
	}
	if _, err := missing.reading(); err == nil {
		t.Fatal("missing was normalized as zero")
	}
	p.Attributes = append(p.Attributes, &radius.AVP{Type: rfc2866.AcctInputOctets_Type, Attribute: radius.Attribute{1}})
	malformed := accountingReceipt(p, n, now)
	if malformed.Fields["Acct-Input-Octets"] != "malformed:01" || malformed.ID == missing.ID || malformed.UploadKnown {
		t.Fatal(malformed)
	}
	rfc2866.AcctInputOctets_Del(p)
	_ = rfc2866.AcctInputOctets_Set(p, 0)
	zero := accountingReceipt(p, n, now)
	if !zero.UploadKnown || zero.Upload != "0" || zero.ID == missing.ID {
		t.Fatal(zero)
	}
	_ = rfc2869.AcctInputGigawords_Set(p, 1)
	large := accountingReceipt(p, n, now)
	e, err := large.reading()
	if err != nil || e.Upload != 4294967296 || e.Download != 0 {
		t.Fatal("RADIUS input must normalize as upload", large, e, err)
	}
	_ = rfc2869.AcctInputGigawords_Set(p, 0xffffffff)
	_ = rfc2866.AcctInputOctets_Set(p, 0xffffffff)
	outside := accountingReceipt(p, n, now)
	if outside.Upload != "18446744073709551615" {
		t.Fatal(outside)
	}
	if _, err = outside.reading(); err == nil {
		t.Fatal("unsigned overflow entered signed ledger")
	}
	start := accountingReceipt(receiptPacket("synthetic-lease", "session", rfc2866.AcctStatusType_Value_Start, nil, nil, nil), n, now)
	if e, err = start.reading(); err != nil || e.Seconds != 0 || e.Upload != 0 || e.Download != 0 {
		t.Fatal("initial Start permits absent zero statistics", e, err)
	}
}

func TestReceiptStartRefusesMalformedAndAmbiguousStatistics(t *testing.T) {
	n := NAS{RouterID: "lab", Network: "C"}
	for _, kind := range []radius.Type{rfc2866.AcctSessionTime_Type, rfc2866.AcctInputOctets_Type, rfc2866.AcctOutputOctets_Type, rfc2869.AcctInputGigawords_Type, rfc2869.AcctOutputGigawords_Type} {
		for _, duplicate := range []bool{false, true} {
			p := receiptPacket("synthetic-lease", "session", rfc2866.AcctStatusType_Value_Start, nil, nil, nil)
			if duplicate {
				p.Add(kind, radius.NewInteger(0))
				p.Add(kind, radius.NewInteger(1))
			} else {
				p.Add(kind, radius.Attribute{1})
			}
			r := accountingReceipt(p, n, time.Now())
			if _, err := r.reading(); err == nil {
				t.Fatalf("Start normalized an invalid statistic: type=%d duplicate=%t receipt=%+v", kind, duplicate, r)
			}
		}
	}
	for _, kind := range []radius.Type{rfc2869.AcctInputGigawords_Type, rfc2869.AcctOutputGigawords_Type} {
		p := receiptPacket("synthetic-lease", "session", rfc2866.AcctStatusType_Value_Start, nil, nil, nil)
		p.Add(kind, radius.NewInteger(1))
		if _, err := accountingReceipt(p, n, time.Now()).reading(); err == nil {
			t.Fatal("Start accepted a high word without its low word")
		}
	}
}

func TestReceiptRetainsDistinctMalformedEvidence(t *testing.T) {
	n := NAS{RouterID: "lab", Network: "C"}
	p := receiptPacket("synthetic-lease", "session", rfc2866.AcctStatusType_Value_Stop, nil, nil, nil)
	p.Add(rfc2866.AcctInputOctets_Type, radius.Attribute{1})
	first := accountingReceipt(p, n, time.Now())
	p.Set(rfc2866.AcctInputOctets_Type, radius.Attribute{2})
	second := accountingReceipt(p, n, time.Now())
	if first.ID == second.ID || first.Fields["Acct-Input-Octets"] == second.Fields["Acct-Input-Octets"] {
		t.Fatal("distinct malformed evidence collapsed into one receipt")
	}
}

func TestReceiptParserDoesNotRetainCredentialsOrArbitraryAttributes(t *testing.T) {
	p := receiptPacket("synthetic-lease", "session", rfc2866.AcctStatusType_Value_Start, nil, nil, nil)
	_ = rfc2865.UserName_SetString(p, "synthetic-private-marker")
	_ = rfc2865.UserPassword_SetString(p, "synthetic-private-marker")
	r := accountingReceipt(p, NAS{RouterID: "lab", Network: "C"}, time.Now())
	raw, err := json.Marshal(r)
	if err != nil || strings.Contains(string(raw), "synthetic-private-marker") || len(r.Fields) != 0 {
		t.Fatal("non-accounting fields escaped the whitelist", r, err)
	}
}

func TestReceiptFromUnenrolledNASDoesNotReachStorage(t *testing.T) {
	s, _, _, addr := protocolFixture(t)
	client := radius.Client{Dialer: net.Dialer{LocalAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.2")}}}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	p := receiptPacket("synthetic-lease", "session", rfc2866.AcctStatusType_Value_Start, nil, nil, nil)
	if reply, err := client.Exchange(ctx, p, addr); err == nil || reply != nil {
		t.Fatal("unknown NAS received an accounting response")
	}
	var count int
	if err := s.sql.QueryRow(`SELECT count(*) FROM subscriber_accounting_receipts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("unknown NAS reached storage", count, err)
	}
}

func TestRealUDPIncompleteStopIsDurablePausedAndRecoveredOnce(t *testing.T) {
	s, svc, a, addr := protocolFixture(t)
	accepted := exchange(t, access("synthetic-password"), addr)
	lease := rfc2865.Class_GetString(accepted)
	if accepted.Code != radius.CodeAccessAccept || lease == "" {
		t.Fatal(accepted)
	}
	exchange(t, receiptPacket(lease, "receipt-session", rfc2866.AcctStatusType_Value_Start, nil, nil, nil), addr)
	partial := receiptPacket(lease, "receipt-session", rfc2866.AcctStatusType_Value_Stop, nil, receiptUint32(100), receiptUint32(200))
	for range 2 {
		if r := exchange(t, partial, addr); r.Code != radius.CodeAccountingResponse {
			t.Fatal(r)
		}
	}
	v, err := s.Snapshot(t.Context(), a.ID, time.Now())
	if err != nil || v.PendingAccounting != 1 || v.Quality != "reconciling" || v.LifetimeSeconds != 0 || v.Decision.Allowed || v.Decision.Limits[0].Reserved == 0 {
		t.Fatal("partial Stop freed credit or hid known usage", v, err)
	}
	receipts, err := s.AccountingReceipts(t.Context(), a.ID)
	if err != nil || len(receipts) != 2 {
		t.Fatal("receipt retries duplicated history", receipts, err)
	}
	other := access("synthetic-password")
	_ = rfc2865.CallingStationID_SetString(other, "02:00:00:00:00:02")
	_ = signResponse(other)
	if r := exchange(t, other, addr); r.Code != radius.CodeAccessReject {
		t.Fatal("unresolved telemetry admitted another device", r)
	}
	// A reconstructed service uses the same durable queue and continues to refuse admission.
	restarted := &RadiusService{Store: NewStore(s.sql), Clients: svc.Clients, Mode: svc.Mode, PilotAccounts: svc.PilotAccounts}
	if pending, err := restarted.Store.Snapshot(t.Context(), a.ID, time.Now()); err != nil || pending.PendingAccounting != 1 {
		t.Fatal(pending, err)
	}
	complete := receiptPacket(lease, "receipt-session", rfc2866.AcctStatusType_Value_Stop, receiptUint32(20), receiptUint32(100), receiptUint32(200))
	exchange(t, complete, addr)
	v, err = s.Snapshot(t.Context(), a.ID, time.Now())
	if err != nil || v.PendingAccounting != 0 || v.LifetimeSeconds != 20 || v.LifetimeDownload != 200 || v.LifetimeUpload != 100 || v.Decision.Limits[0].Remaining != 3580 {
		t.Fatal(v, err)
	}
	newAccept := exchange(t, other, addr)
	newLease := rfc2865.Class_GetString(newAccept)
	if newAccept.Code != radius.CodeAccessAccept || newLease == lease {
		t.Fatal(newAccept)
	}
	newStart := receiptPacket(newLease, "new-session", rfc2866.AcctStatusType_Value_Start, nil, nil, nil)
	_ = rfc2865.CallingStationID_SetString(newStart, "02:00:00:00:00:02")
	exchange(t, newStart, addr)
	exchange(t, partial, addr)
	v, err = s.Snapshot(t.Context(), a.ID, time.Now())
	if err != nil || v.PendingAccounting != 0 || v.LifetimeSeconds != 20 {
		t.Fatal(v, err)
	}
	found := false
	for _, session := range v.Sessions {
		if session.ID == newLease {
			found = session.State == "active"
		}
	}
	if !found {
		t.Fatal("late partial Stop closed a new session", v)
	}
}

func TestReceiptCommitAndDebitFailuresNeverAcknowledgeEarly(t *testing.T) {
	for _, table := range []string{"subscriber_accounting_receipts", "subscriber_events"} {
		t.Run(table, func(t *testing.T) {
			s, svc, a, _ := protocolFixture(t)
			start := time.Now().Truncate(time.Second)
			grant, err := s.Reserve(t.Context(), a.ID, "lab", "C", "02:00:00:00:00:01", "fault-receipt", start)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Account(t.Context(), Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: "02:00:00:00:00:01", SessionID: "failure-session", Status: "start", At: start}); err != nil {
				t.Fatal(err)
			}
			conn, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			handled := make(chan struct{}, 4)
			server := &radius.PacketServer{SecretSource: svc, Handler: radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) { svc.ServeRADIUS(w, r); handled <- struct{}{} })}
			go server.Serve(conn)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = server.Shutdown(ctx)
			})
			if _, err = s.sql.Exec(`CREATE TRIGGER synthetic_receipt_fail BEFORE INSERT ON ` + table + ` BEGIN SELECT RAISE(ABORT,'synthetic receipt failure'); END`); err != nil {
				t.Fatal(err)
			}
			p := receiptPacket(grant.LeaseID, "failure-session", rfc2866.AcctStatusType_Value_Stop, receiptUint32(20), receiptUint32(100), receiptUint32(200))
			result := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_, err := radius.Exchange(ctx, p, conn.LocalAddr().String())
				result <- err
			}()
			select {
			case <-handled:
			case <-time.After(5 * time.Second):
				t.Fatal("packet was not processed")
			}
			if err := <-result; err == nil {
				t.Fatal("acknowledged failed durable accounting")
			}
			view, err := s.Snapshot(t.Context(), a.ID, start.Add(time.Second))
			if err != nil || view.LifetimeSeconds != 0 {
				t.Fatal("failed debit partially committed", view, err)
			}
			if table == "subscriber_events" && view.PendingAccounting != 1 {
				t.Fatal("failed debit lost its durable receipt", view)
			}
			if _, err = s.sql.Exec(`DROP TRIGGER synthetic_receipt_fail`); err != nil {
				t.Fatal(err)
			}
			if response := exchange(t, p, conn.LocalAddr().String()); response.Code != radius.CodeAccountingResponse {
				t.Fatal(response)
			}
			<-handled
			view, err = s.Snapshot(t.Context(), a.ID, start.Add(20*time.Second))
			if err != nil || view.PendingAccounting != 0 || view.LifetimeSeconds != 20 || view.LifetimeDownload != 200 {
				t.Fatal("retry failed to recover once", view, err)
			}
		})
	}
}

func TestReceiptMarkersHaveRouterScopeAndNoBalanceEffects(t *testing.T) {
	s, _, a, addr := protocolFixture(t)
	before, err := s.Snapshot(t.Context(), a.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []rfc2866.AcctStatusType{rfc2866.AcctStatusType_Value_AccountingOn, rfc2866.AcctStatusType_Value_AccountingOff} {
		p := radius.New(radius.CodeAccountingRequest, []byte(testSecret))
		_ = rfc2866.AcctStatusType_Set(p, status)
		for range 2 {
			if r := exchange(t, p, addr); r.Code != radius.CodeAccountingResponse {
				t.Fatal(r)
			}
		}
	}
	var count int
	if err = s.sql.QueryRow(`SELECT count(*) FROM subscriber_accounting_receipts WHERE router_id='lab' AND lease_id IS NULL AND state='applied'`).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	after, err := s.Snapshot(t.Context(), a.ID, time.Now())
	if err != nil || after.LifetimeSeconds != before.LifetimeSeconds || after.PendingAccounting != 0 {
		t.Fatal(after, err)
	}
}
