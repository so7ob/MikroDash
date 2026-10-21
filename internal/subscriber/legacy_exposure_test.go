package subscriber

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLegacyAdmissionRetryCommitsExposureUpgradeAtomically(t *testing.T) {
	c := resetCases(t)[0]
	s, _, a, grant, start := crossingFixture(t, c.period, c.edge, func(p *Plan) { p.MaxSessions = 2; p.Policies[0].Limits[0].Amount = 100 })
	if err := s.Account(t.Context(), Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: "device", SessionID: "session", Status: "start", At: start}); err != nil {
		t.Fatal(err)
	}
	// Reproduce an existing active grant whose old version only held its current
	// period. No accounting event or counter is removed to simulate the upgrade.
	if _, err := s.sql.Exec(`UPDATE subscriber_leases SET decision=json_remove(decision,'$.timingBasis') WHERE id=?`, grant.LeaseID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.sql.Exec(`DELETE FROM subscriber_reservations WHERE lease_id=? AND period_start>=?`, grant.LeaseID, c.edge.Unix()); err != nil {
		t.Fatal(err)
	}
	before := reservationRows(t, s)
	if _, err := s.sql.Exec(`CREATE TRIGGER synthetic_legacy_upgrade_failure BEFORE INSERT ON subscriber_audit WHEN NEW.action='admission.legacy.exposure-retained' BEGIN SELECT RAISE(ABORT,'synthetic legacy upgrade audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	now := start.Add(time.Second)
	if _, err := s.Reserve(t.Context(), a.ID, "lab", "C", "device", "calendar-admission", now); err == nil {
		t.Fatal("audit failure accepted legacy retry")
	}
	if reservationRows(t, s) != before {
		t.Fatal("legacy audit failure partially upgraded reservations")
	}
	if _, err := s.sql.Exec(`DROP TRIGGER synthetic_legacy_upgrade_failure`); err != nil {
		t.Fatal(err)
	}
	retry, err := s.Reserve(t.Context(), a.ID, "lab", "C", "device", "calendar-admission", now)
	if err != nil || retry.LeaseID != grant.LeaseID || retry.Decision.Timeout != grant.Decision.Timeout {
		t.Fatal("legacy retry changed issued identity or timeout", retry, err)
	}
	for _, expected := range []struct {
		id     string
		amount Quantity
	}{{"time", 30}, {"download", grant.Download}} {
		var amount Quantity
		if err = s.sql.QueryRow(`SELECT amount FROM subscriber_reservations WHERE lease_id=? AND limit_id=? AND period_start=?`, grant.LeaseID, expected.id, c.edge.Unix()).Scan(&amount); err != nil || amount < expected.amount {
			t.Fatal("successful retry lost the legacy future exposure upgrade", expected, amount, err)
		}
	}
	var raw string
	if err = s.sql.QueryRow(`SELECT decision FROM subscriber_leases WHERE id=?`, grant.LeaseID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored Admission
	if err = json.Unmarshal([]byte(raw), &stored); err != nil || stored.TimingBasis != admissionTimingBasis {
		t.Fatal("legacy timing upgrade did not persist", stored, err)
	}
	state := reservationRows(t, s)
	if _, err = s.Reserve(t.Context(), a.ID, "lab", "C", "device", "calendar-admission", now); err != nil || reservationRows(t, s) != state {
		t.Fatal("second retry duplicated exposure", err)
	}
	var auditCount int
	if err = s.sql.QueryRow(`SELECT count(*) FROM subscriber_audit WHERE action='admission.legacy.exposure-retained' AND target=?`, grant.LeaseID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatal("legacy upgrade audit not exactly once", auditCount, err)
	}
	snapshot, err := s.Snapshot(t.Context(), a.ID, now)
	if err != nil || snapshot.LifetimeSeconds != 0 || snapshot.LifetimeDownload != 0 || len(snapshot.Sessions) != 1 {
		t.Fatal("legacy retry changed confirmed use or created another session", snapshot, err)
	}
}
