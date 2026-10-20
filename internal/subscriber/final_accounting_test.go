package subscriber

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

func TestMissingFinalAccountingBlocksAdmissionAndOffersReviewWithoutHolds(t *testing.T) {
	for _, unlimited := range []bool{false, true} {
		t.Run(map[bool]string{false: "limited", true: "unlimited"}[unlimited], func(t *testing.T) {
			s, svc, account, grant, start := liveFixture(t, func(p *Plan) {
				if unlimited {
					for i := range p.Policies {
						for j := range p.Policies[i].Limits {
							p.Policies[i].Limits[j].State = "unlimited"
							p.Policies[i].Limits[j].Amount = 0
						}
					}
				}
			})
			at := start.Add(61 * time.Second)
			if err := s.ObserveRouter(t.Context(), RouterObservation{RouterID: "lab", At: at, Complete: true}, svc.Clients); err != nil {
				t.Fatal(err)
			}
			before, err := s.Snapshot(t.Context(), account.ID, at)
			if err != nil || len(before.Sessions) != 1 || before.Sessions[0].State != "stopped" || before.PendingAccounting != 1 || before.Decision.Allowed {
				t.Fatal("missing final accounting allowed unknown credit", before, err)
			}
			if _, err = s.Reserve(t.Context(), account.ID, "lab", "C", "new-device", "after-missing-stop", at); !errors.Is(err, ErrDenied) {
				t.Fatal("admission call site bypassed final accounting", err)
			}
			reviews, err := s.SessionReviews(t.Context(), account.ID)
			if err != nil || len(reviews) != 1 || reviews[0].LeaseID != grant.LeaseID || (unlimited && reviews[0].UnresolvedLimits != 0) {
				t.Fatal("missing final reading has no reasoned recovery path", reviews, err)
			}
			if _, err = s.sql.Exec(`CREATE TRIGGER synthetic_final_failure BEFORE INSERT ON subscriber_audit WHEN NEW.action='accounting.missing-stop.review' BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`); err != nil {
				t.Fatal(err)
			}
			reading := FinalReading{LeaseID: grant.LeaseID, Seconds: 20, Download: 100, Upload: 10, Reason: "Synthetic approved final reading", Evidence: "Synthetic independent final sample"}
			if err = s.ReconcileMissingStop(t.Context(), "synthetic-admin", reading, at); err == nil {
				t.Fatal("failed audit accepted finality")
			}
			failed, err := s.Snapshot(t.Context(), account.ID, at)
			if err != nil || !reflect.DeepEqual(before, failed) {
				t.Fatal("failed review released credit or finality gate", err)
			}
			if _, err = s.sql.Exec(`DROP TRIGGER synthetic_final_failure`); err != nil {
				t.Fatal(err)
			}
			if err = s.ReconcileMissingStop(t.Context(), "synthetic-admin", reading, at); err != nil {
				t.Fatal(err)
			}
			after, err := s.Snapshot(t.Context(), account.ID, at)
			if err != nil || after.PendingAccounting != 0 || !after.Decision.Allowed || after.LifetimeSeconds != 20 || after.LifetimeDownload != 100 || after.Subscription.ID != before.Subscription.ID || after.Subscription.ExpiresAt != before.Subscription.ExpiresAt {
				t.Fatal("approved final reading did not restore the same entitlement", after, err)
			}
			reviews, err = s.SessionReviews(t.Context(), account.ID)
			if err != nil || len(reviews) != 0 {
				t.Fatal("settled finality still needs review", reviews, err)
			}
		})
	}
}

func TestMissingFinalAccountingRejectsRealUDPAfterDatabaseReopen(t *testing.T) {
	s, svc, account, address := protocolFixture(t)
	start := time.Now().Add(-63 * time.Second).Truncate(time.Second)
	plans, err := s.Plans(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := plans[0]
	p.Version++
	p.Policies[0].Limits[0].Period = Period{Kind: "none", Zone: "UTC", Anchor: start.Add(-time.Hour)}
	if err = s.SavePlan(t.Context(), "synthetic-admin", "non-renewing finality fixture", p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Assign(t.Context(), "synthetic-admin", account.ID, p.ID, p.Version, "non-renewing finality fixture", start); err != nil {
		t.Fatal(err)
	}
	device := "02:00:00:00:00:01"
	grant, err := s.Reserve(t.Context(), account.ID, "lab", "C", device, "old-session", start)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Account(t.Context(), Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: device, SessionID: "old-session", Status: "start", At: start}); err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveRouter(t.Context(), RouterObservation{RouterID: "lab", At: time.Now(), Complete: true}, svc.Clients); err != nil {
		t.Fatal(err)
	}
	var seq int
	var name, file string
	if err = s.sql.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &file); err != nil {
		t.Fatal(err)
	}
	if err = s.sql.Close(); err != nil {
		t.Fatal(err)
	}
	h, err := sql.Open("sqlite", protocolFixtureURI(file))
	if err != nil {
		t.Fatal(err)
	}
	h.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = h.Close() })
	s = NewStore(h)
	svc.Store = s
	if reply := exchange(t, access("synthetic-password"), address); reply.Code != radius.CodeAccessReject {
		t.Fatal("reopened receiver admitted unresolved final accounting", reply.Code)
	}
	final := receiptPacket(grant.LeaseID, "old-session", rfc2866.AcctStatusType_Value_Stop, receiptUint32(58), receiptUint32(20), receiptUint32(100))
	for range 2 {
		if reply := exchange(t, final, address); reply.Code != radius.CodeAccountingResponse {
			t.Fatal("covering final Stop was not durably acknowledged")
		}
	}
	settled, err := s.Snapshot(t.Context(), account.ID, time.Now())
	if err != nil || settled.PendingAccounting != 0 || settled.LifetimeSeconds != 58 || settled.LifetimeDownload != 100 || settled.LifetimeUpload != 20 || settled.Decision.Limits[0].Remaining != 3542 {
		t.Fatal("final Stop lost credit, doubled counters or kept the gate", settled, err)
	}
	reply := exchange(t, access("synthetic-password"), address)
	newLease := rfc2865.Class_GetString(reply)
	if reply.Code != radius.CodeAccessAccept || rfc2865.Class_GetString(reply) == grant.LeaseID {
		t.Fatal("settled account cannot obtain a distinct bounded lease", reply.Code)
	}
	if out := exchange(t, final, address); out.Code != radius.CodeAccountingResponse {
		t.Fatal("late final retransmission refused")
	}
	after, err := s.Snapshot(t.Context(), account.ID, time.Now())
	if err != nil || after.LifetimeSeconds != 58 || after.Subscription.ID != settled.Subscription.ID {
		t.Fatal("old Stop changed a new lease or subscription", after, err)
	}
	var state string
	if err = s.sql.QueryRow(`SELECT state FROM subscriber_leases WHERE id=?`, newLease).Scan(&state); err != nil || state != "reserved" {
		t.Fatal("old Stop closed the newly admitted lease", state, err)
	}
}

func TestMissingFinalAccountingDoesNotBlockAnotherAccount(t *testing.T) {
	s, svc, account, _, start := liveFixture(t)
	at := start.Add(61 * time.Second)
	if err := s.ObserveRouter(t.Context(), RouterObservation{RouterID: "lab", At: at, Complete: true}, svc.Clients); err != nil {
		t.Fatal(err)
	}
	other, _, err := s.CreateAccount(t.Context(), "synthetic-admin", "synthetic-other", "Independent Unicode: اختبار", "synthetic-only-password", "account isolation", 0)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot(t.Context(), account.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	p := before.Subscription.Plan
	if _, err = s.Assign(t.Context(), "synthetic-admin", other.ID, p.ID, p.Version, "independent entitlement", at); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reserve(t.Context(), other.ID, "lab", "C", "other-device", "independent-grant", at); err != nil {
		t.Fatal("one account's missing Stop blocked an unrelated account", err)
	}
	after, err := s.Snapshot(t.Context(), account.ID, at)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("another account changed unresolved credit", err)
	}
}
