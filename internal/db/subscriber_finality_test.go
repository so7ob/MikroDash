package db

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"mikrodash/internal/subscriber"
)

func TestFinalityIndexMigrationPreservesExistingMissingStopAndLedger(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := d.Subscribers()
	a, _, err := s.CreateAccount(t.Context(), "synthetic-admin", "synthetic-finality", "Synthetic migration", "synthetic-only-password", "migration fixture", 0)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Truncate(time.Second)
	p := subscriber.Plan{ID: "finality", Version: 1, Name: "Synthetic finality", ValidSeconds: 86400, Start: "first-login", MaxSessions: 1, Networks: []string{"C"}, Down: 8000000, Up: 2000000, Speeds: []subscriber.Speed{{ID: "bounded", Name: "Bounded", Down: 8000000, Up: 2000000}}, Policies: []subscriber.Policy{{ID: "time", Limits: []subscriber.Limit{{ID: "seconds", Kind: "seconds", State: "limited", Amount: 3600, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: at.Add(-time.Hour)}}}}}}
	if err = s.SavePlan(t.Context(), "synthetic-admin", "migration fixture", p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Assign(t.Context(), "synthetic-admin", a.ID, p.ID, 1, "migration fixture", at); err != nil {
		t.Fatal(err)
	}
	grant, err := s.Reserve(t.Context(), a.ID, "synthetic", "C", "device", "migration-fixture", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Account(t.Context(), subscriber.Accounting{LeaseID: grant.LeaseID, RouterID: "synthetic", Network: "C", Device: "device", SessionID: "synthetic", Status: "start", At: at}); err != nil {
		t.Fatal(err)
	}
	at = at.Add(61 * time.Second)
	if err = s.ObserveRouter(t.Context(), subscriber.RouterObservation{RouterID: "synthetic", At: at, Complete: true}, []subscriber.NAS{{RouterID: "synthetic", Network: "C", CalledStationID: "hs-lab"}}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot(t.Context(), a.ID, at)
	if err != nil || before.PendingAccounting != 1 {
		t.Fatal("actual missing finality fixture not reached", before, err)
	}
	for _, index := range []string{"subscriber_final_stop", "subscriber_absence_finality", "subscriber_subscription_account"} {
		if _, err = d.sql.Exec("DROP INDEX " + index); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = d.sql.Exec(`DELETE FROM schema_version WHERE version>41`); err != nil {
		t.Fatal(err)
	}
	if count, err := d.Migrate(); err != nil || count != schemaVersion-41 {
		t.Fatal("index-only migration failed", count, err)
	}
	after, err := s.Snapshot(t.Context(), a.ID, at)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("index migration rewrote ledger/history/entitlement", err)
	}
	if count, err := d.Migrate(); err != nil || count != 0 {
		t.Fatal("migration was not idempotent", count, err)
	}
	for _, query := range []struct{ sql, index string }{
		{`SELECT 1 FROM subscriber_events WHERE lease_id='synthetic' AND status='stop'`, "subscriber_final_stop"},
		{`SELECT 1 FROM subscriber_reconciliations WHERE lease_id='synthetic' AND actor='service:subscriber'`, "subscriber_absence_finality"},
	} {
		var id, parent, unused int
		var detail string
		if err = d.sql.QueryRow("EXPLAIN QUERY PLAN "+query.sql).Scan(&id, &parent, &unused, &detail); err != nil || !strings.Contains(detail, query.index) || !strings.Contains(detail, "SEARCH") {
			t.Fatal("finality lookup scans the full accounting history", detail, err)
		}
	}
}
