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

	"github.com/evanw/esbuild/pkg/api"
	"mikrodash/internal/pages"
)

func mountSubscriberAdminBrowserFixture(t *testing.T, mux *http.ServeMux, root string) {
	t.Helper()
	bundle := api.Build(api.BuildOptions{EntryPoints: []string{filepath.Join(root, "web/test/browser/subscriber-admin-entry.ts")}, Bundle: true, Write: false, Format: api.FormatIIFE, Platform: api.PlatformBrowser, LogLevel: api.LogLevelSilent})
	if len(bundle.Errors) != 0 || len(bundle.OutputFiles) != 1 {
		t.Fatal(bundle.Errors)
	}
	var markup strings.Builder
	for _, p := range pages.All {
		if pages.IsSubscriber(p.Key) || p.Key == "interfaces" || p.Key == "vlans" || p.Key == "bridges" {
			content, err := os.ReadFile(filepath.Join(root, "web/src/ui/page-"+p.Key+".html"))
			if err != nil {
				t.Fatal(err)
			}
			markup.Write(content)
		}
	}
	shell, err := os.ReadFile(filepath.Join(root, "web/src/ui/shell.html"))
	if err != nil {
		t.Fatal(err)
	}
	navStart, navEnd := strings.Index(string(shell), "<nav id="), strings.Index(string(shell), "</nav>")
	burgerStart := strings.Index(string(shell), `<button id="burgerBtn"`)
	burgerEnd := strings.Index(string(shell)[burgerStart:], "</button>") + burgerStart + 9
	page := string(shell)[navStart:navEnd+6] + string(shell)[burgerStart:burgerEnd] + `<div id="navOverlay"></div><main id="main"><h1 class="card-title">Synthetic administration fixture</h1>` + markup.String() + `</main>`
	mux.HandleFunc("GET /api/nav-prefs", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"grouped": true}) })
	mux.HandleFunc("POST /api/nav-prefs", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"ok": true}) })
	fixturePage := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="en" dir="ltr" data-bs-theme="dark" data-theme="dark"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Synthetic subscriber administration</title><link rel="stylesheet" href="/vendor/tabler.min.css"><link rel="stylesheet" href="/vendor/fonts/fonts.css"><link rel="stylesheet" href="/css/app-fonts.css"><link rel="stylesheet" href="/app.css"></head><body>`+page+`<script src="/admin-test.js"></script></body></html>`)
	}
	mux.HandleFunc("GET /{$}", fixturePage)
	for _, p := range pages.All {
		if pages.IsSubscriber(p.Key) {
			mux.HandleFunc("GET /"+p.URL(), fixturePage)
		}
	}

	mux.HandleFunc("GET /admin-test.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(bundle.OutputFiles[0].Contents)
	})
	mux.Handle("GET /", http.FileServer(http.Dir(filepath.Join(root, "web/public"))))
}

func TestSubscriberCredentialBrowserAcceptance(t *testing.T) {
	if os.Getenv("SUBSCRIBER_BROWSER_MODULE") == "" || os.Getenv("SUBSCRIBER_EVIDENCE_DIR") == "" {
		t.Fatal("configure browser module and evidence directory")
	}
	s, mux, id := reconciliationFixture(t)
	ledger := s.auditDB.Subscribers()
	if _, err := ledger.EditAccountName(t.Context(), "synthetic-admin-id", id, "Synthetic fixture", "Synthetic اسم <script>window.credentialNameExecuted=true</script>", "Synthetic browser fixture"); err != nil {
		t.Fatal(err)
	}
	other, _, err := ledger.CreateAccount(t.Context(), "synthetic-admin-id", "synthetic-other-credential", "Synthetic other account", "synthetic-other-password", "Synthetic scope fixture", 0)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	s.staticDir = filepath.Join(root, "web/public")
	mountSubscriberAdminBrowserFixture(t, mux, root)
	edge := httptest.NewTLSServer(mux)
	defer edge.Close()
	portal := httptest.NewTLSServer(s.subscriberPortal(t.TempDir()))
	defer portal.Close()
	s.subscribers.cfg.PortalOrigin = portal.URL
	command := exec.Command("node", filepath.Join(root, "tools/subscriber-credential-browser.mjs"))
	command.Env = append(os.Environ(), "SUBSCRIBER_TEST_URL="+edge.URL, "SUBSCRIBER_PORTAL_TEST_URL="+portal.URL, "SUBSCRIBER_ACCOUNT_ID="+id, "SUBSCRIBER_OTHER_ACCOUNT_ID="+other.ID)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("credential browser: %v\n%s", err, output)
	}
	t.Log(string(output))
	account, err := ledger.AccountByID(t.Context(), id)
	if err != nil || account.CredentialRevision != 3 {
		t.Fatal("browser resets did not advance exactly three epochs", err)
	}
	if _, err = ledger.Authenticate(t.Context(), "synthetic-account", "synthetic-browser-final-password"); err != nil {
		t.Fatal("final browser credential not persisted", err)
	}
	if _, err = ledger.Authenticate(t.Context(), other.Username, "synthetic-other-password"); err != nil {
		t.Fatal("browser touched unrelated account", err)
	}
	reader, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: s.auditDB.Path(), RawQuery: "mode=ro"}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	rows, err := reader.Query(`SELECT actor,reason FROM subscriber_audit WHERE action='account.credential.reset' AND target=? ORDER BY id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var actor, reason string
		if rows.Scan(&actor, &reason) != nil {
			t.Fatal("audit unreadable")
		}
		if actor != "synthetic-admin-id" || strings.Contains(reason, `"password"`) || strings.Contains(reason, "synthetic-browser-") {
			t.Fatal("browser reset audit lost authority or leaked secret")
		}
		count++
	}
	if err = rows.Err(); err != nil || count != 3 {
		t.Fatal("browser audit count mismatch", count, err)
	}
}
