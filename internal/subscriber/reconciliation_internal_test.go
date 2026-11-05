package subscriber

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestMissingStopAdministrativeReviewIsAtomicAndAudited(t *testing.T) {
	s, svc, a, grant, start := liveFixture(t)
	absent := RouterObservation{RouterID: "lab", Version: "synthetic-7", At: start.Add(61 * time.Second), Complete: true, Sessions: []ObservedSession{}}
	if err := s.ObserveRouter(t.Context(), absent, svc.Clients); err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot(t.Context(), a.ID, absent.At)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.sql.Exec(`CREATE TRIGGER synthetic_review_fail BEFORE INSERT ON subscriber_audit WHEN NEW.action='accounting.missing-stop.review' BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	in := FinalReading{LeaseID: grant.LeaseID, Seconds: 20, Download: 100, Reason: "Approved final reading", Evidence: "Synthetic lab record"}
	if err = s.ReconcileMissingStop(t.Context(), "admin", in, absent.At); err == nil {
		t.Fatal("audit failure did not abort settlement")
	}
	failed, err := s.Snapshot(t.Context(), a.ID, absent.At)
	if err != nil || failed.Version != before.Version {
		t.Fatal("review partially committed", failed, err)
	}
	if _, err = s.sql.Exec(`DROP TRIGGER synthetic_review_fail`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = s.ReconcileMissingStop(t.Context(), "admin", in, absent.At); err != nil {
			t.Fatal(err)
		}
	}
	settled, err := s.Snapshot(t.Context(), a.ID, absent.At)
	if err != nil || settled.LifetimeSeconds != 20 || settled.LifetimeDownload != 100 || settled.Decision.Limits[0].Reserved != 0 || settled.Decision.Limits[0].Remaining != 3580 {
		t.Fatal(settled, err)
	}
	reviews, err := s.SessionReviews(t.Context(), a.ID)
	if err != nil || len(reviews) != 0 {
		t.Fatal(reviews, err)
	}
	var audits int
	if err = s.sql.QueryRow(`SELECT count(*) FROM subscriber_audit WHERE action='accounting.missing-stop.review'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("review duplicated", audits, err)
	}
	in.Seconds = 19
	if err = s.ReconcileMissingStop(t.Context(), "admin", in, absent.At); err == nil {
		t.Fatal("regressing approved counters accepted")
	}
}
func TestLegacyConservativeMigrationPreservesBoundsAndReviewHistory(t *testing.T) {
	s, _, a, _ := protocolFixture(t)
	var sub string
	if err := s.sql.QueryRow(`SELECT id FROM subscriber_subscriptions WHERE account_id=?`, a.ID).Scan(&sub); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	plans, err := s.Plans(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	start, _, err := plans[0].Policies[0].Limits[0].Period.Bounds(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.sql.Exec(`INSERT INTO subscriber_buckets VALUES(?,?,?,?,?)`, sub, "time", start.Unix(), 200, "boundary-conservative"); err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = s.sql.Exec(ReconciliationSchema); err != nil {
			t.Fatal(err)
		}
	}
	migrated, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil || migrated.Decision.Limits[0].Used != 0 || migrated.Decision.Limits[0].Provisional != 200 || migrated.Decision.Limits[0].Remaining != before.Decision.Limits[0].Remaining {
		t.Fatal("migration invented credit or measured usage", migrated, err)
	}
	records, err := s.LegacyExposures(t.Context(), a.ID)
	if err != nil || len(records) != 1 || records[0].Amount != 200 {
		t.Fatal(records, err)
	}
	for range 2 {
		if err = s.ResolveLegacyExposure(t.Context(), "admin", records[0].ID, "approved distribution", "synthetic source", 80, now); err != nil {
			t.Fatal(err)
		}
	}
	settled, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil || settled.Decision.Limits[0].Used != 80 || settled.Decision.Limits[0].Provisional != 0 || settled.LifetimeSeconds != before.LifetimeSeconds {
		t.Fatal(settled, err)
	}
	records, err = s.LegacyExposures(t.Context(), a.ID)
	if err != nil || records[0].Amount != 200 || records[0].SettledAmount != 80 {
		t.Fatal("original evidence was changed", records, err)
	}
	if err = s.ResolveLegacyExposure(t.Context(), "admin", records[0].ID, "approved distribution", "synthetic source", 81, now); err == nil {
		t.Fatal("settled evidence overwritten")
	}
}

func TestByteGapApprovalRollsBackAndConcurrentReviewsCannotOverwrite(t *testing.T) {
	s, _, a, _ := protocolFixture(t)
	plans, err := s.Plans(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := plans[0]
	p.Version++
	start := time.Date(2030, 10, 31, 23, 59, 50, 0, time.UTC)
	p.Policies = []Policy{{ID: "distribution", Limits: []Limit{{ID: "bytes", Kind: "download", State: "limited", Amount: 1000, Action: "alert", Period: Period{Kind: "daily", Zone: "UTC", Anchor: start.Truncate(24 * time.Hour)}}}}}
	if err = s.SavePlan(t.Context(), "admin", "synthetic distribution fixture", p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Assign(t.Context(), "admin", a.ID, p.ID, p.Version, "synthetic distribution fixture", start); err != nil {
		t.Fatal(err)
	}
	grant, err := s.Reserve(t.Context(), a.ID, "lab", "C", "device", "synthetic distribution fixture", start)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Account(t.Context(), Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: "device", SessionID: "session", Status: "stop", Seconds: 20, Download: 100, At: start.Add(20 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	gaps, err := s.ByteGaps(t.Context(), a.ID)
	if err != nil || len(gaps) != 1 || len(gaps[0].Periods) != 2 {
		t.Fatal(gaps, err)
	}
	gap := gaps[0]
	allocations := []GapAllocation{{PeriodStart: gap.Periods[0], Amount: 40}, {PeriodStart: gap.Periods[1], Amount: 60}}
	if _, err = s.sql.Exec(`CREATE TRIGGER synthetic_gap_audit_failure BEFORE INSERT ON subscriber_audit WHEN NEW.action='accounting.byte-gap.settle' BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = s.ResolveByteGap(t.Context(), "admin", gap.ID, "synthetic approval", "synthetic evidence", allocations, start.Add(time.Minute)); err == nil {
		t.Fatal("audit failure did not abort byte settlement")
	}
	failed, err := s.ByteGaps(t.Context(), a.ID)
	if err != nil || !failed[0].ResolvedAt.IsZero() {
		t.Fatal("approval partially committed", failed, err)
	}
	view, err := s.Snapshot(t.Context(), a.ID, start.Add(20*time.Second))
	if err != nil || view.Decision.Limits[0].Used != 0 || view.Decision.Limits[0].Provisional != 100 || view.LifetimeDownload != 100 {
		t.Fatal("rollback changed usage or holds", view, err)
	}
	if _, err = s.sql.Exec(`DROP TRIGGER synthetic_gap_audit_failure`); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for i := range 2 {
		go func() {
			results <- s.ResolveByteGap(t.Context(), fmt.Sprint("admin-", i), gap.ID, "synthetic approval", "synthetic evidence", allocations, start.Add(time.Minute))
		}()
	}
	successes, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal(successes, conflicts)
	}
	var audits int
	if err = s.sql.QueryRow(`SELECT count(*) FROM subscriber_audit WHERE action='accounting.byte-gap.settle'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatal(audits, err)
	}
	view, err = s.Snapshot(t.Context(), a.ID, start.Add(20*time.Second))
	if err != nil || view.LifetimeDownload != 100 || view.Decision.Limits[0].Used != 60 || view.Decision.Limits[0].Provisional != 0 {
		t.Fatal(view, err)
	}
}
