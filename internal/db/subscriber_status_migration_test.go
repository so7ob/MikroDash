package db

import (
	"reflect"
	"strings"
	"testing"
)

func TestStatusRevisionIndexMigrationPreservesAccountsCredentialsAndAudit(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := d.Subscribers()
	account, code, err := store.CreateAccount(t.Context(), "synthetic-admin", "synthetic-status-migration", "Synthetic Unicode: قسيمة", "", "Synthetic migration fixture", 24)
	if err != nil || code == "" {
		t.Fatal("missing voucher fixture", err)
	}
	for _, status := range []string{"suspended", "active"} {
		if err = store.SetStatus(t.Context(), "synthetic-admin", account.ID, status, "Synthetic historical status"); err != nil {
			t.Fatal(err)
		}
	}
	before, err := store.Accounts(t.Context())
	if err != nil || len(before) != 1 || before[0].StatusRevision == 0 {
		t.Fatal("missing versioned status history", err)
	}
	var audits int
	if err = d.sql.QueryRow(`SELECT count(*) FROM subscriber_audit`).Scan(&audits); err != nil || audits < 3 {
		t.Fatal("missing positive audit fixture", audits, err)
	}
	if _, err = d.sql.Exec(`DROP INDEX subscriber_status_revision; DELETE FROM schema_version WHERE version>43;`); err != nil {
		t.Fatal(err)
	}
	if count, err := d.Migrate(); err != nil || count != 1 {
		t.Fatal("v43 status-index migration failed", count, err)
	}
	after, err := store.Accounts(t.Context())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("index migration rewrote identity or status history", err)
	}
	authenticated, err := store.Authenticate(t.Context(), "", code)
	if err != nil || authenticated != after[0] {
		t.Fatal("index migration changed the voucher credential", err)
	}
	var retained int
	if err = d.sql.QueryRow(`SELECT count(*) FROM subscriber_audit`).Scan(&retained); err != nil || retained != audits {
		t.Fatal("index migration erased or fabricated audit", retained, err)
	}
	rows, err := d.sql.Query(`EXPLAIN QUERY PLAN SELECT MAX(id) FROM subscriber_audit WHERE target=? AND action IN ('account.active','account.suspended','account.status.batch')`, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	used := false
	plans := []string{}
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, detail)
		used = used || strings.Contains(detail, "SEARCH subscriber_audit USING ") && strings.Contains(detail, "INDEX subscriber_status_revision (target=?)")
	}
	err = rows.Err()
	rows.Close()
	if err != nil || !used {
		t.Fatal("status revision lookup did not use the target index", plans, err)
	}
	if count, err := d.Migrate(); err != nil || count != 0 {
		t.Fatal("status-index migration was not idempotent", count, err)
	}
}
