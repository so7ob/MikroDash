package subscriber

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func auditFixture(t *testing.T) (*Store, Account) {
	t.Helper()
	s, a := credentialFixture(t)
	if _, err := s.sql.Exec(`DELETE FROM subscriber_audit`); err != nil {
		t.Fatal(err)
	}
	return s, a
}
func appendAudit(t *testing.T, s *Store, id int64, action, target, reason string, at int64) {
	t.Helper()
	if _, err := s.sql.Exec(`INSERT INTO subscriber_audit(id,actor,action,target,reason,created_at) VALUES(?,?,?,?,?,?)`, id, "synthetic-admin", action, target, reason, at); err != nil {
		t.Fatal(err)
	}
}

func TestAccountAuditReadsCommittedAccountAndHistoricalSubscriptionEvents(t *testing.T) {
	s, a := credentialFixture(t)
	at := time.Now()
	if _, err := s.EditAccountName(t.Context(), "synthetic-admin", a.ID, a.Name, "Synthetic اسم <script>", "Synthetic name correction"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(t.Context(), "synthetic-admin", a.ID, "suspended", "Synthetic suspension"); err != nil {
		t.Fatal(err)
	}
	current, err := s.AccountByID(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetStatusBatch(t.Context(), "synthetic-admin", "active", "Synthetic batch", []AccountStatusExpectation{{AccountID: a.ID, ExpectedStatus: "suspended", ExpectedRevision: &current.StatusRevision}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResetCredential(t.Context(), "synthetic-admin", a.ID, "synthetic-audit-new-password", "Synthetic reset", 0); err != nil {
		t.Fatal(err)
	}
	if err = s.ChooseSpeed(t.Context(), a.ID, "fast", at); err != nil {
		t.Fatal(err)
	}
	second, err := s.Assign(t.Context(), "synthetic-admin", a.ID, "synthetic-plan", 1, "Synthetic fresh subscription", at)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Snapshot(t.Context(), a.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := s.AccountByID(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.sql.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	page, err := s.AccountAudit(t.Context(), AccountAuditQuery{AccountID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	subscriptions := map[string]bool{}
	for _, event := range page.Events {
		counts[event.Action]++
		if event.SubscriptionID != "" {
			subscriptions[event.SubscriptionID] = true
			if event.PlanID != "synthetic-plan" || event.PlanVersion != 1 {
				t.Fatal("wrong immutable assignment metadata")
			}
		}
	}
	want := map[string]int{"account.create": 1, "subscription.assign": 2, "account.name.edit": 1, "account.suspended": 1, "account.status.batch": 1, "account.credential.reset": 1, "speed.request": 1}
	if !reflect.DeepEqual(counts, want) || len(subscriptions) != 2 || !subscriptions[second] {
		t.Fatal("missing committed historical audit", counts, subscriptions)
	}
	raw, _ := json.Marshal(page)
	for _, secret := range []string{"synthetic-audit-new-password", "synthetic-old-password", "password_hash", "voucher_hash", "salt"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("audit revealed credential material")
		}
	}
	profile, err := s.AccountAudit(t.Context(), AccountAuditQuery{AccountID: a.ID, Category: "profile"})
	if err != nil || len(profile.Events) != 2 || !strings.Contains(profile.Events[0].Reason, "اسم") {
		t.Fatal("profile evidence lost original Unicode", err)
	}
	credentials, err := s.AccountAudit(t.Context(), AccountAuditQuery{AccountID: a.ID, Category: "credentials"})
	if err != nil || len(credentials.Events) != 1 || credentials.Events[0].EvidenceState != "complete" {
		t.Fatal("credential envelope unavailable", err)
	}
	oldStatus, err := s.StatusHistory(t.Context(), a.ID, 0)
	if err != nil || len(oldStatus.Events) != 2 {
		t.Fatal("audit affected existing status history", err)
	}
	if _, err = s.sql.Exec(`UPDATE subscriber_accounts SET name='forbidden'`); err == nil {
		t.Fatal("readonly fixture accepted a write")
	}
	// Snapshot uses the existing immediate transaction, so restore write-lock
	// capability only after independently proving the audit reader is query-only.
	if _, err = s.sql.Exec(`PRAGMA query_only=OFF`); err != nil {
		t.Fatal(err)
	}
	after, err := s.Snapshot(t.Context(), a.ID, at)
	if err != nil || !reflect.DeepEqual(snapshot, after) {
		t.Fatal("audit changed entitlement/usage", err)
	}
	afterMeta, err := s.AccountByID(t.Context(), a.ID)
	if err != nil || !reflect.DeepEqual(meta, afterMeta) {
		t.Fatal("audit changed profile/credential epochs", err)
	}
}

func TestAccountAuditKeysetBoundariesAndInterveningWrites(t *testing.T) {
	for _, count := range []int{0, 1, 49, 50, 51, 100, 101} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s, a := auditFixture(t)
			const base int64 = 9007199254740993
			for i := 0; i < count; i++ {
				appendAudit(t, s, base+int64(i*3), "account.active", a.ID, fmt.Sprintf("Synthetic reason %d", i), 1000-int64(i))
			}
			var before Quantity
			seen := 0
			for {
				page, err := s.AccountAudit(t.Context(), AccountAuditQuery{AccountID: a.ID, Before: before})
				if err != nil {
					t.Fatal(err)
				}
				expected := count - seen
				if expected > 50 {
					expected = 50
				}
				if page.Events == nil || len(page.Events) != expected {
					t.Fatal("wrong page boundary", len(page.Events), expected)
				}
				for _, event := range page.Events {
					index := count - 1 - seen
					if event.ID != Quantity(base+int64(index*3)) || event.Reason != fmt.Sprintf("Synthetic reason %d", index) {
						t.Fatal("repeated or skipped event", event.ID, index)
					}
					seen++
				}
				if before == 0 {
					raw, _ := json.Marshal(page)
					if count > 0 && !strings.Contains(string(raw), fmt.Sprintf(`"id":"%d"`, base+int64((count-1)*3))) {
						t.Fatal("rounded audit ID")
					}
					appendAudit(t, s, base+1000, "account.suspended", a.ID, "Synthetic intervening write", time.Now().Unix())
				}
				if seen == count {
					if page.NextBefore != nil {
						t.Fatal("terminal page advertised more")
					}
					break
				}
				if page.NextBefore == nil || before != 0 && *page.NextBefore >= before {
					t.Fatal("nonadvancing cursor")
				}
				before = *page.NextBefore
			}
		})
	}
}

func TestAccountAuditOwnershipDomainsFiltersAndIndexedQueries(t *testing.T) {
	s, a := auditFixture(t)
	other, _, err := s.CreateAccount(t.Context(), "synthetic-admin", "synthetic-foreign-audit", "Other", "synthetic-foreign-password", "Synthetic foreign fixture", 0)
	if err != nil {
		t.Fatal(err)
	}
	foreignSub, err := s.Assign(t.Context(), "synthetic-admin", other.ID, "synthetic-plan", 1, "Synthetic foreign subscription", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately collide IDs across entity domains. Ownership still follows
	// the event action and persisted relationship, not the matching string alone.
	if _, err = s.sql.Exec(`UPDATE subscriber_subscriptions SET id=? WHERE account_id=?`, other.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.sql.Exec(`UPDATE subscriber_subscriptions SET id=? WHERE id=?`, a.ID, foreignSub); err != nil {
		t.Fatal(err)
	}
	if _, err = s.sql.Exec(`DELETE FROM subscriber_audit`); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	appendAudit(t, s, 100, "account.create", a.ID, "Synthetic selected creation", at.Add(-time.Second).Unix())
	appendAudit(t, s, 101, "account.name.edit", a.ID, `{"reason":"Synthetic selected edit","before":"Old","after":"New"}`, at.Unix())
	appendAudit(t, s, 102, "subscription.assign", other.ID, "Synthetic owned subscription", at.Add(23*time.Hour).Unix())
	appendAudit(t, s, 103, "speed.request", other.ID, "Synthetic owned speed", at.Add(24*time.Hour).Unix())
	appendAudit(t, s, 104, "subscription.assign", a.ID, "Synthetic foreign subscription private", at.Unix())
	appendAudit(t, s, 105, "account.create", other.ID, "Synthetic foreign profile private", at.Unix())
	appendAudit(t, s, 106, "account.password", a.ID, "Synthetic unsupported private", at.Unix())
	appendAudit(t, s, 107, "plan.version", a.ID, "Synthetic shared plan private", at.Unix())
	page, err := s.AccountAudit(t.Context(), AccountAuditQuery{AccountID: a.ID, From: "2026-11-14", Until: "2026-11-14"})
	if err != nil || len(page.Events) != 2 || page.Events[0].ID != 102 || page.Events[1].ID != 101 {
		t.Fatal("UTC date boundary or ownership failed", page, err)
	}
	all, err := s.AccountAudit(t.Context(), AccountAuditQuery{AccountID: a.ID})
	if err != nil || len(all.Events) != 4 {
		t.Fatal("domain collision leaked unrelated history", all, err)
	}
	for category, want := range map[string]int{"profile": 2, "status": 0, "credentials": 0, "subscriptions": 1, "speed": 1} {
		page, err = s.AccountAudit(t.Context(), AccountAuditQuery{AccountID: a.ID, Category: category})
		if err != nil || len(page.Events) != want {
			t.Fatal(category, len(page.Events), err)
		}
	}
	for _, q := range []AccountAuditQuery{{}, {AccountID: a.ID, Before: -1}, {AccountID: a.ID, Category: "account"}, {AccountID: a.ID, From: "2026-11-15", Until: "2026-11-14"}, {AccountID: a.ID, Until: "2026-02-30"}, {AccountID: a.ID, From: "26-11-14"}, {AccountID: a.ID, From: "0000-01-01"}, {AccountID: a.ID, From: "2026-11-14", Until: "0001-01-01"}} {
		if _, err = s.AccountAudit(t.Context(), q); !errors.Is(err, ErrAuditQuery) {
			t.Fatal("invalid query accepted", q, err)
		}
	}
	// Large unrelated histories must not become a full audit-table scan.
	if _, err = s.sql.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<10000) INSERT INTO subscriber_audit(id,actor,action,target,reason,created_at) SELECT 1000+x,'synthetic-admin','account.create','synthetic-unrelated-'||x,'Synthetic unrelated',0 FROM n`); err != nil {
		t.Fatal(err)
	}
	query, args, err := auditStatement(AccountAuditQuery{AccountID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.sql.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail + "\n")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Count(plan.String(), "SEARCH a USING INDEX subscriber_audit_target") != 2 || !strings.Contains(plan.String(), "SEARCH s USING INDEX subscriber_subscription_account") {
		t.Fatal("audit is not using bounded ownership/target indexes", plan.String())
	}
	rows.Close()
	page, err = s.AccountAudit(t.Context(), AccountAuditQuery{AccountID: a.ID})
	if err != nil || !reflect.DeepEqual(all, page) {
		t.Fatal("unrelated history changed selected results", err)
	}
}

func TestAccountAuditBoundsEvidenceAndHidesUnsafeCredentialEnvelopes(t *testing.T) {
	s, a := auditFixture(t)
	appendAudit(t, s, 100, "account.create", a.ID, strings.Repeat("ح", AccountAuditEvidenceRunes+100), 0)
	valid := `{"reason":"Synthetic safe reset","beforeRevision":"9007199254740993","afterRevision":"9007199254740994"}`
	appendAudit(t, s, 101, "account.credential.reset", a.ID, valid, 0)
	for i, raw := range []string{
		`{"reason":"Synthetic safe","beforeRevision":"0","afterRevision":"1","password":"synthetic-private-material"}`,
		`{"reason":"synthetic-private-material","reason":"Synthetic safe","beforeRevision":"0","afterRevision":"1"}`,
		`{"Reason":"synthetic-private-material","reason":"Synthetic safe","beforeRevision":"0","afterRevision":"1"}`,
		`{"reason":"synthetic-private-material"}`, `synthetic-private-material`,
		`{"reason":"synthetic-private-material","beforeRevision":0,"afterRevision":1}`,
		`{"reason":"synthetic-private-material","beforeRevision":"0","afterRevision":"2"}`,
		`{"reason":"synthetic-private-material","beforeRevision":"9223372036854775807","afterRevision":"0"}`,
		`{"reason":"synthetic-private-material","beforeRevision":"0","afterRevision":"1"} {}`,
	} {
		appendAudit(t, s, 102+int64(i), "account.credential.reset", a.ID, raw, 0)
	}
	page, err := s.AccountAudit(t.Context(), AccountAuditQuery{AccountID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range page.Events {
		switch event.ID {
		case 100:
			if event.EvidenceState != "truncated" || len([]rune(event.Reason)) != AccountAuditEvidenceRunes {
				t.Fatal("unbounded or unlabelled evidence")
			}
		case 101:
			if event.Reason != valid || event.EvidenceState != "complete" {
				t.Fatal("valid original envelope changed")
			}
		default:
			if event.Reason != "" || event.EvidenceState != "unavailable" {
				t.Fatal("unsafe credential envelope returned")
			}
		}
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "synthetic-private-material") {
		t.Fatal("unsafe credential evidence leaked")
	}
}

func TestAccountAuditHasNoPhantomEventsAfterWriteRollback(t *testing.T) {
	s, a := auditFixture(t)
	before, err := s.AccountAudit(t.Context(), AccountAuditQuery{AccountID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.sql.Exec(`CREATE TRIGGER synthetic_audit_failure BEFORE INSERT ON subscriber_audit BEGIN SELECT RAISE(FAIL,'synthetic failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.EditAccountName(t.Context(), "synthetic-admin", a.ID, a.Name, "Uncommitted", "Synthetic rollback"); err == nil {
		t.Fatal("audit fault did not roll back")
	}
	after, err := s.AccountAudit(t.Context(), AccountAuditQuery{AccountID: a.ID})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rollback produced phantom history", err)
	}
	current, err := s.AccountByID(t.Context(), a.ID)
	if err != nil || current.Name != a.Name {
		t.Fatal("failed audit changed account", err)
	}
}

func TestAccountAuditPreservesNULAndMultibyteEvidenceBoundaries(t *testing.T) {
	s, a := auditFixture(t)
	original := "Synthetic original\x00after NUL"
	appendAudit(t, s, 100, "account.create", a.ID, original, 0)
	appendAudit(t, s, 101, "account.create", a.ID, "x"+strings.Repeat("🙂", 9000), 0)
	page, err := s.AccountAudit(t.Context(), AccountAuditQuery{AccountID: a.ID})
	if err != nil || len(page.Events) != 2 {
		t.Fatal(err)
	}
	if page.Events[1].Reason != original || page.Events[1].EvidenceState != "complete" {
		t.Fatal("audit silently changed an embedded NUL record")
	}
	if page.Events[0].EvidenceState != "truncated" || len([]rune(page.Events[0].Reason)) != AccountAuditEvidenceRunes {
		t.Fatal("multibyte evidence was lost or split instead of bounded")
	}
}
