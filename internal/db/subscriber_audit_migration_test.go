package db

import (
	"encoding/json"
	"mikrodash/internal/subscriber"
	"reflect"
	"testing"
)

func TestAccountAuditMigrationPreservesHistoricalRecords(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	account, _, err := d.Subscribers().CreateAccount(t.Context(), "synthetic-admin", "synthetic-audit-upgrade", "Synthetic اسم", "synthetic-upgrade-password", "Original audit", 0)
	if err != nil {
		t.Fatal(err)
	}
	before, err := d.Subscribers().AccountAudit(t.Context(), subscriber.AccountAuditQuery{AccountID: account.ID})
	if err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(before)
	if _, err = d.sql.Exec(`DROP INDEX subscriber_audit_target;DELETE FROM schema_version WHERE version>46`); err != nil {
		t.Fatal(err)
	}
	if n, err := d.Migrate(); err != nil || n != schemaVersion-46 {
		t.Fatal("index migration failed", n, err)
	}
	after, err := d.Subscribers().AccountAudit(t.Context(), subscriber.AccountAuditQuery{AccountID: account.ID})
	encoded, _ := json.Marshal(after)
	if err != nil || !reflect.DeepEqual(original, encoded) {
		t.Fatal("index migration rewrote original history", err)
	}
	if n, err := d.Migrate(); err != nil || n != 0 {
		t.Fatal("index migration not repeatable", n, err)
	}
	if _, err = d.Subscribers().Authenticate(t.Context(), account.Username, "synthetic-upgrade-password"); err != nil {
		t.Fatal("audit migration changed credential", err)
	}
}
