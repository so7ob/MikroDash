package db

import (
	"reflect"
	"testing"
	"time"

	"mikrodash/internal/subscriber"
)

func TestCredentialMigrationPreservesExistingSecretsAndRevokesUnboundTickets(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ledger := d.Subscribers()
	account, code, err := ledger.CreateAccount(t.Context(), "synthetic-admin", "synthetic-migration-voucher", "Synthetic حساب", "", "Synthetic pre-migration fixture", 24)
	if err != nil {
		t.Fatal(err)
	}
	token, err := ledger.IssueTicket(t.Context(), subscriber.TicketRequest{AccountID: account.ID, RouterID: "synthetic-router", Network: "C", Device: "02:00:00:00:00:99", ClientAddress: "192.0.2.99"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	before, err := ledger.Accounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var salt, hash, voucher string
	if err = d.sql.QueryRow(`SELECT salt,password_hash,voucher_hash FROM subscriber_accounts WHERE id=?`, account.ID).Scan(&salt, &hash, &voucher); err != nil {
		t.Fatal(err)
	}
	if _, err = d.sql.Exec(`DROP TABLE subscriber_ticket_credentials;DROP TABLE subscriber_credential_revisions;DELETE FROM schema_version WHERE version>45`); err != nil {
		t.Fatal(err)
	}
	if err = ledger.CheckReady(t.Context()); err == nil {
		t.Fatal("old schema passed authentication readiness without credential proofs")
	}
	if n, err := d.Migrate(); err != nil || n != schemaVersion-45 {
		t.Fatal("credential migration failed", n, err)
	}
	if err = ledger.CheckReady(t.Context()); err != nil {
		t.Fatal("migrated credential schema is not ready", err)
	}
	after, err := ledger.Accounts(t.Context())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("migration changed account metadata", err)
	}
	var newSalt, newHash, newVoucher string
	if err = d.sql.QueryRow(`SELECT salt,password_hash,voucher_hash FROM subscriber_accounts WHERE id=?`, account.ID).Scan(&newSalt, &newHash, &newVoucher); err != nil || salt != newSalt || hash != newHash || voucher != newVoucher {
		t.Fatal("migration changed existing credential material", err)
	}
	if _, err = ledger.Authenticate(t.Context(), "", code); err != nil {
		t.Fatal("migration disabled existing voucher", err)
	}
	if _, err = ledger.RedeemTicket(t.Context(), token, subscriber.TicketBinding{RouterID: "synthetic-router", Network: "C", Device: "02:00:00:00:00:99", Username: account.Username}, "synthetic-replay", time.Now()); err == nil {
		t.Fatal("pre-epoch ticket authenticated after migration")
	}
	var retained int
	if err = d.sql.QueryRow(`SELECT count(*) FROM subscriber_connection_flows`).Scan(&retained); err != nil || retained != 1 {
		t.Fatal("migration removed historical flow", retained, err)
	}
	if n, err := d.Migrate(); err != nil || n != 0 {
		t.Fatal("migration is not repeatable", n, err)
	}
}
