//go:build subscriberbrowser

package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"mikrodash/internal/subscriber"
)

// The browser exercises the shipped page module against actual protected HTTP
// handlers and a real SQLite ledger. Only the NAS data is synthetic.
func TestSubscriberAdminReconciliationBrowserAcceptance(t *testing.T) {
	if os.Getenv("SUBSCRIBER_BROWSER_MODULE") == "" || os.Getenv("SUBSCRIBER_EVIDENCE_DIR") == "" {
		t.Fatal("configure browser module and evidence directory")
	}
	s, mux, id := reconciliationFixture(t)
	ledger := s.auditDB.Subscribers()
	s.subscribers = nil // Administration must work before portal/RADIUS enrollment.
	plans, err := ledger.Plans(t.Context())
	if err != nil || len(plans) != 1 {
		t.Fatal(plans, err)
	}
	p := plans[0]
	p.Version = 2
	p.ValidSeconds = 7 * 86400
	midnight := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	p.Policies = []subscriber.Policy{{ID: "synthetic-distribution", Limits: []subscriber.Limit{{ID: "download", Kind: "download", State: "limited", Amount: 1000, Action: "alert", Period: subscriber.Period{Kind: "daily", Zone: "UTC", Anchor: midnight.Add(-24 * time.Hour)}}}}}
	if err = ledger.SavePlan(t.Context(), "synthetic-admin-id", "synthetic browser fixture", p); err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.Assign(t.Context(), "synthetic-admin-id", id, p.ID, p.Version, "synthetic browser fixture", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err = ledger.CreateAccount(t.Context(), "synthetic-admin-id", "synthetic-account-two", "Unicode fixture: اختبار", "synthetic-password", "synthetic browser fixture", 0); err != nil {
		t.Fatal(err)
	}
	start := midnight.Add(-10 * time.Second)
	grant, err := ledger.Reserve(t.Context(), id, "synthetic-router", "C", "02:00:00:00:00:41", "synthetic-request", start)
	if err != nil {
		t.Fatal(err)
	}
	if err = ledger.Account(t.Context(), subscriber.Accounting{LeaseID: grant.LeaseID, RouterID: "synthetic-router", Network: "C", Device: "02:00:00:00:00:41", SessionID: "synthetic-session", Status: "stop", Source: "radius", At: start.Add(20 * time.Second), Seconds: 20, Download: 100}); err != nil {
		t.Fatal(err)
	}
	// A late partial Stop contradicts the old final byte count by one byte.
	// Keep it visible until the browser approves an evidence-backed correction.
	receipt := subscriber.AccountingReceipt{ID: "synthetic-browser-receipt", LeaseID: grant.LeaseID, RouterID: "synthetic-router", Network: "C", Device: "02:00:00:00:00:41", SessionID: "synthetic-session", Status: "stop", Seconds: "20", TimeKnown: true, Download: "101", DownloadKnown: true, At: start.Add(21 * time.Second), ReceivedAt: start.Add(21 * time.Second), Fields: map[string]string{"Acct-Session-Time": "20", "Acct-Output-Octets": "101"}}
	if ack, err := ledger.ReceiveAccounting(t.Context(), receipt); err != nil || !ack {
		t.Fatal(ack, err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bundle := api.Build(api.BuildOptions{EntryPoints: []string{filepath.Join(root, "web/test/browser/subscriber-admin-entry.ts")}, Bundle: true, Write: false, Format: api.FormatIIFE, Platform: api.PlatformBrowser, LogLevel: api.LogLevelSilent})
	if len(bundle.Errors) != 0 || len(bundle.OutputFiles) != 1 {
		t.Fatal(bundle.Errors)
	}
	markup, err := os.ReadFile(filepath.Join(root, "web/src/ui/page-subscribers.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Replace(string(markup), `class="page-view"`, `class="page-view active"`, 1)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en" dir="ltr" data-bs-theme="dark" data-theme="dark"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Synthetic subscriber administration</title><link rel="stylesheet" href="/vendor/tabler.min.css"><link rel="stylesheet" href="/vendor/fonts/fonts.css"><link rel="stylesheet" href="/css/app-fonts.css"><link rel="stylesheet" href="/app.css"></head><body><main id="main"><h1 class="card-title">Synthetic administration fixture</h1>`+page+`</main><script src="/admin-test.js"></script></body></html>`)
	})
	mux.HandleFunc("GET /admin-test.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundle.OutputFiles[0].Contents)
	})
	mux.Handle("GET /", http.FileServer(http.Dir(filepath.Join(root, "web/public"))))
	server := httptest.NewTLSServer(mux)
	defer server.Close()
	command := exec.Command("node", filepath.Join(root, "tools/subscriber-admin-browser.mjs"))
	command.Env = append(os.Environ(), "SUBSCRIBER_TEST_URL="+server.URL, "SUBSCRIBER_ACCOUNT_ID="+id)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("admin browser acceptance: %v\n%s", err, output)
	}
	t.Log(string(output))
	var changes, batches int
	// Inspect this test's isolated SQLite file read-only, independently of the
	// writer; do not expose a production raw-SQL handle for browser assertions.
	reader, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: s.auditDB.Path(), RawQuery: "mode=ro"}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err = reader.QueryRow(`SELECT count(*),count(DISTINCT json_extract(reason,'$.batchId')) FROM subscriber_audit WHERE action='account.status.batch'`).Scan(&changes, &batches); err != nil || changes != 4 || batches != 2 {
		t.Fatal("browser status batches did not match durable audit", changes, batches, err)
	}
	gaps, err := ledger.ByteGaps(t.Context(), id)
	if err != nil || len(gaps) != 1 || gaps[0].ResolvedAt.IsZero() {
		t.Fatal(gaps, err)
	}
	snapshot, err := ledger.Snapshot(t.Context(), id, time.Now())
	if err != nil || snapshot.LifetimeDownload != 101 || snapshot.LifetimeSeconds != 20 || snapshot.PendingAccounting != 0 {
		t.Fatal(snapshot, err)
	}
}
