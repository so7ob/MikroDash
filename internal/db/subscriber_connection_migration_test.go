package db

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"mikrodash/internal/subscriber"
)

func TestConnectionMigrationPreservesLegacyCredentialsBalancesAndActiveLeases(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := d.Subscribers()
	account, _, err := s.CreateAccount(t.Context(), "synthetic-admin", "synthetic-migration", "Synthetic Unicode: سحب", "synthetic-only-password", "connection migration fixture", 0)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Truncate(time.Second)
	plan := subscriber.Plan{ID: "synthetic-connection", Version: 1, Name: "Synthetic migration", ValidSeconds: 86400, Start: "first-login", MaxSessions: 1, Networks: []string{"C"}, Down: 8000000, Up: 2000000, Speeds: []subscriber.Speed{{ID: "bounded", Name: "Bounded", Down: 8000000, Up: 2000000}}, Policies: []subscriber.Policy{{ID: "time", Limits: []subscriber.Limit{{ID: "seconds", Kind: "seconds", State: "limited", Amount: 3600, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: at.Add(-time.Hour)}}}}}}
	if err = s.SavePlan(t.Context(), "synthetic-admin", "migration fixture", plan); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Assign(t.Context(), "synthetic-admin", account.ID, plan.ID, 1, "migration fixture", at); err != nil {
		t.Fatal(err)
	}
	grant, err := s.Reserve(t.Context(), account.ID, "lab", "C", "device", "synthetic-legacy-grant", at)
	if err != nil {
		t.Fatal(err)
	}
	event := subscriber.Accounting{LeaseID: grant.LeaseID, RouterID: "lab", Network: "C", Device: "device", SessionID: "synthetic-legacy-session", Status: "start", At: at}
	if err = s.Account(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	event.Status = "interim"
	event.At = at.Add(20 * time.Second)
	event.Seconds = 20
	event.Download = 50
	event.Upload = 10
	if err = s.Account(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot(t.Context(), account.ID, event.At)
	if err != nil {
		t.Fatal(err)
	}
	oldToken := "mdt_" + subscriber.ID()
	if _, err = d.sql.Exec(`INSERT INTO subscriber_tickets(token_hash,account_id,network,device,expires_at) VALUES(?,?,?,?,?)`, subscriber.Digest(oldToken), account.ID, "C", "device", at.Add(30*time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	var flows int
	if err = d.sql.QueryRow(`SELECT count(*) FROM subscriber_connection_flows`).Scan(&flows); err != nil || flows != 0 {
		t.Fatal("unexpected migration fixture flow", flows, err)
	}
	// Reconstruct the actual v42 shape: only the new empty metadata table goes.
	if _, err = d.sql.Exec(`DROP TABLE subscriber_connection_flows`); err != nil {
		t.Fatal(err)
	}
	if _, err = d.sql.Exec(`DELETE FROM schema_version WHERE version>42`); err != nil {
		t.Fatal(err)
	}
	if count, err := d.Migrate(); err != nil || count != schemaVersion-42 {
		t.Fatal("connection metadata migration failed", count, err)
	}
	after, err := s.Snapshot(t.Context(), account.ID, event.At)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("migration rewrote balances, subscription or active Internet lease", err)
	}
	auth, err := s.Authenticate(t.Context(), account.Username, "synthetic-only-password")
	if err != nil || auth.Name != account.Name || auth.ID != account.ID {
		t.Fatal("credentials or Unicode data changed", auth, err)
	}
	if _, err = s.RedeemTicket(t.Context(), oldToken, subscriber.TicketBinding{RouterID: "lab", Network: "C", Device: "device", Username: account.Username}, "synthetic-old-ticket", event.At); !errors.Is(err, subscriber.ErrDenied) {
		t.Fatal("unbound legacy ticket was silently trusted", err)
	}
	var prior string
	if err = d.sql.QueryRow(`SELECT request_key FROM subscriber_tickets WHERE token_hash=?`, subscriber.Digest(oldToken)).Scan(&prior); err != nil || prior != "" {
		t.Fatal("legacy ticket was rewritten or consumed", prior, err)
	}
	event.At = at.Add(21 * time.Second)
	event.Seconds = 21
	if err = s.Account(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	continued, err := s.Snapshot(t.Context(), account.ID, event.At)
	if err != nil || continued.LifetimeSeconds != 21 || len(continued.Sessions) != 1 || continued.Sessions[0].State != "active" {
		t.Fatal("existing network accounting stopped after migration", continued, err)
	}
	if count, err := d.Migrate(); err != nil || count != 0 {
		t.Fatal("connection migration is not idempotent", count, err)
	}
}
