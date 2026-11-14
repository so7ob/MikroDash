//go:build subscriberbrowser

package server

import (
	"database/sql"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"mikrodash/internal/subscriber"
)

func TestSubscriberAccountAuditBrowserAcceptance(t *testing.T) {
	if os.Getenv("SUBSCRIBER_BROWSER_MODULE") == "" || os.Getenv("SUBSCRIBER_EVIDENCE_DIR") == "" {
		t.Fatal("configure browser module and evidence directory")
	}
	s, mux, id := reconciliationFixture(t)
	s.subscribers = nil
	s.auth.SetLocal(func(token string) (*Session, bool) {
		switch token {
		case "synthetic-token":
			return &Session{AuthMode: "modern", Username: "synthetic-admin"}, true
		case "synthetic-viewer-token":
			return &Session{AuthMode: "modern", Username: "synthetic-viewer"}, true
		}
		return nil, false
	})
	ledger := s.auditDB.Subscribers()
	writer, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: s.auditDB.Path()}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	tx, err := writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	const base int64 = 9007199254740993
	for i := int64(0); i < 53; i++ {
		action := "account.active"
		if i%2 == 1 {
			action = "account.suspended"
		}
		if _, err = tx.Exec(`INSERT INTO subscriber_audit(id,actor,action,target,reason,created_at) VALUES(?,?,?,?,?,?)`, base+i, "synthetic-admin-id", action, id, fmt.Sprintf("Synthetic older status %d", i), time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC).Unix()); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.EditAccountName(t.Context(), "synthetic-admin-id", id, "Synthetic fixture", "Synthetic اسم <script>window.syntheticAuditExecuted=true</script>", "سبب <script>window.syntheticAuditExecuted=true</script>"); err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.ResetCredential(t.Context(), "synthetic-admin-id", id, "synthetic-audit-browser-password", "Synthetic reviewed browser reset", 0); err != nil {
		t.Fatal(err)
	}
	if err = ledger.ChooseSpeed(t.Context(), id, "fast", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.Assign(t.Context(), "synthetic-admin-id", id, "test", 1, "Synthetic reviewed new period", time.Now()); err != nil {
		t.Fatal(err)
	}
	other, _, err := ledger.CreateAccount(t.Context(), "synthetic-admin-id", "synthetic-audit-other", "Synthetic other", "synthetic-other-password", "Synthetic foreign private reason", 0)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	before, err := ledger.Snapshot(t.Context(), id, at)
	if err != nil {
		t.Fatal(err)
	}
	beforeMeta, err := ledger.AccountByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	var beforeCount int
	if err = writer.QueryRow(`SELECT count(*) FROM subscriber_audit`).Scan(&beforeCount); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	mountSubscriberAdminBrowserFixture(t, mux, root)
	edge := httptest.NewTLSServer(mux)
	defer edge.Close()
	command := exec.Command("node", filepath.Join(root, "tools/subscriber-audit-browser.mjs"))
	command.Env = append(os.Environ(), "SUBSCRIBER_TEST_URL="+edge.URL, "SUBSCRIBER_ACCOUNT_ID="+id, "SUBSCRIBER_OTHER_ACCOUNT_ID="+other.ID)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("account audit browser: %v\n%s", err, output)
	}
	t.Log(string(output))
	after, err := ledger.Snapshot(t.Context(), id, at)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("read-only browser changed entitlement or accounting", err)
	}
	afterMeta, err := ledger.AccountByID(t.Context(), id)
	if err != nil || !reflect.DeepEqual(beforeMeta, afterMeta) {
		t.Fatal("read-only browser changed account or credential revision", err)
	}
	var afterCount int
	if err = writer.QueryRow(`SELECT count(*) FROM subscriber_audit`).Scan(&afterCount); err != nil || afterCount != beforeCount {
		t.Fatal("read-only browser produced audit mutations", err)
	}
	if _, err = ledger.Authenticate(t.Context(), "synthetic-account", "synthetic-audit-browser-password"); err != nil {
		t.Fatal("audit browser changed current authentication", err)
	}
	page, err := ledger.AccountAudit(t.Context(), subscriber.AccountAuditQuery{AccountID: id})
	if err != nil || len(page.Events) != 50 || page.NextBefore == nil {
		t.Fatal("browser did not retain the original history page", err)
	}
}
