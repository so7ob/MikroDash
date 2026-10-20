package subscriber

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAccountNameEditAuditsUnicodeWithoutChangingEntitlement(t *testing.T) {
	s, _, account, _ := protocolFixture(t)
	now := time.Now().Truncate(time.Second)
	before, err := s.Snapshot(t.Context(), account.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	const name = "  Synthetic Unicode: اسم المشترك <fixture>  "
	edited, err := s.EditAccountName(t.Context(), "synthetic-admin", account.ID, account.Name, name, "Synthetic name correction")
	if err != nil || edited.ID != account.ID || edited.Username != account.Username || edited.Status != account.Status || edited.Name != name {
		t.Fatal(edited, err)
	}
	after, err := s.Snapshot(t.Context(), account.ID, now)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("metadata changed an entitlement or balance", err)
	}
	if authenticated, err := s.Authenticate(t.Context(), account.Username, "synthetic-password"); err != nil || authenticated != edited {
		t.Fatal("existing credentials changed", authenticated, err)
	}
	var raw, actor string
	if err = s.sql.QueryRow(`SELECT actor,reason FROM subscriber_audit WHERE action='account.name.edit' AND target=?`, account.ID).Scan(&actor, &raw); err != nil {
		t.Fatal(err)
	}
	var history map[string]string
	if err = json.Unmarshal([]byte(raw), &history); err != nil || actor != "synthetic-admin" || history["before"] != account.Name || history["after"] != name || history["reason"] != "Synthetic name correction" {
		t.Fatal(history, actor, err)
	}
	if _, err = s.EditAccountName(t.Context(), "synthetic-admin", account.ID, account.Name, "Stale replacement", "Synthetic stale edit"); !errors.Is(err, ErrConflict) {
		t.Fatal("stale overwrite accepted", err)
	}
	if _, err = s.EditAccountName(t.Context(), "synthetic-admin", account.ID, name, name, "Synthetic no-op"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.sql.QueryRow(`SELECT count(*) FROM subscriber_audit WHERE action='account.name.edit'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed/no-op edit created a second change", count, err)
	}
}

func TestAccountNameEditRollsBackWhenAuditCannotCommit(t *testing.T) {
	s, _, account, _ := protocolFixture(t)
	if _, err := s.sql.Exec(`CREATE TRIGGER synthetic_name_audit_failure BEFORE INSERT ON subscriber_audit WHEN NEW.action='account.name.edit' BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditAccountName(t.Context(), "synthetic-admin", account.ID, account.Name, "Uncommitted name", "Synthetic fault"); err == nil {
		t.Fatal("reported success without audit")
	}
	retained, err := s.AccountByID(t.Context(), account.ID)
	if err != nil || retained != account {
		t.Fatal("failed audit left a partial edit", retained, err)
	}
}

func TestAccountNameEditValidatesReasonPresenceAndUnicode(t *testing.T) {
	s, _, account, _ := protocolFixture(t)
	for _, tc := range []struct{ actor, id, name, reason string }{
		{"synthetic-admin", account.ID, "Name", " "},
		{" ", account.ID, "Name", "Synthetic reason"},
		{"synthetic-admin", "missing", "Name", "Synthetic reason"},
		{"synthetic-admin", account.ID, strings.Repeat("x", 257), "Synthetic reason"},
		{"synthetic-admin", account.ID, string([]byte{0xff}), "Synthetic reason"},
	} {
		if _, err := s.EditAccountName(t.Context(), tc.actor, tc.id, account.Name, tc.name, tc.reason); err == nil {
			t.Fatal("invalid edit accepted")
		}
	}
	voucher, code, err := s.CreateAccount(t.Context(), "synthetic-admin", "synthetic-voucher-name", "Synthetic voucher", "", "Synthetic creation", 16)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := s.EditAccountName(t.Context(), "synthetic-admin", voucher.ID, voucher.Name, "Synthetic voucher: قسيمة", "Synthetic voucher edit")
	if err != nil {
		t.Fatal(err)
	}
	if authenticated, err := s.Authenticate(t.Context(), "", code); err != nil || authenticated != edited {
		t.Fatal("display edit changed a voucher credential", err)
	}
}
