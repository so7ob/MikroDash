package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mikrodash/internal/activity"
	"mikrodash/internal/db"
)

func TestActivityDashboardRequiresBothPagesOnTheSameRouter(t *testing.T) {
	s, mux, _ := activityTestServer(t)
	cookie := "mikrodash_sid=activity-test-token"
	path := "/api/activity/dashboard?routerId=r1"
	if w := activityRequest(mux, "GET", path, "", ""); w.Code != 401 {
		t.Fatal("unauthenticated", w.Code)
	}
	if w := activityRequest(mux, "GET", "/api/activity/dashboard?routerId=r2", "", cookie); w.Code != 403 {
		t.Fatal("other router", w.Code)
	}
	if _, err := s.auditDB.DeleteGrantsForScope("router", "r1"); err != nil {
		t.Fatal(err)
	}
	role, err := s.auditDB.CreateRole(map[string]any{"name": "Activity dashboard test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.auditDB.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: "activity-user", RoleID: role.ID, ScopeType: "router", ScopeID: "r1"}); err != nil {
		t.Fatal(err)
	}
	setPages := func(pages []db.RolePage) {
		t.Helper()
		if _, err := s.auditDB.SetRolePages(role.ID, pages); err != nil {
			t.Fatal(err)
		}
	}
	for _, page := range []string{"activity", "dashboard"} {
		setPages([]db.RolePage{{Page: page, Access: "read"}})
		if w := activityRequest(mux, "GET", path, "", cookie); w.Code != 403 {
			t.Fatalf("only %s granted: %d", page, w.Code)
		}
	}
	// Dashboard on another router must not satisfy the selected router's check.
	other, err := s.auditDB.CreateRole(map[string]any{"name": "Other router dashboard"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.auditDB.SetRolePages(other.ID, []db.RolePage{{Page: "dashboard", Access: "read"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.auditDB.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: "activity-user", RoleID: other.ID, ScopeType: "router", ScopeID: "r2"}); err != nil {
		t.Fatal(err)
	}
	setPages([]db.RolePage{{Page: "activity", Access: "read"}})
	if w := activityRequest(mux, "GET", path, "", cookie); w.Code != 403 {
		t.Fatal("cross-router Dashboard grant accepted", w.Code)
	}
	if w := activityRequest(mux, "GET", "/api/activity/query?routerId=r1", "", cookie); w.Code != 200 {
		t.Fatal("standalone Activity requires Dashboard", w.Code)
	}
	setPages([]db.RolePage{{Page: "activity", Access: "read"}, {Page: "dashboard", Access: "read"}})
	now := time.Now().UnixMilli()
	events := []activity.Event{{RouterID: "r1", SourceKey: "current", Kind: "dns", Domain: "chatgpt.com", At: now, Received: now}, {RouterID: "r1", SourceKey: "older", Kind: "dns", Domain: "chatgpt.com", At: now - 25*3600000, Received: now}, {RouterID: "r2", SourceKey: "other", Kind: "dns", Domain: "facebook.com", At: now, Received: now}}
	if err = s.auditDB.PersistActivity(events, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	w := activityRequest(mux, "GET", path, "", cookie)
	var out activity.Result
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.RouterID != "r1" || out.Totals.DNS != 1 || out.Filter.To-out.Filter.From != 24*3600000 {
		t.Fatalf("scoped 24-hour response: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("sensitive dashboard response can be cached")
	}
	setPages([]db.RolePage{{Page: "dashboard", Access: "read"}})
	if w = activityRequest(mux, "GET", path, "", cookie); w.Code != 403 {
		t.Fatal("revoked Activity still readable", w.Code)
	}
}

// Real dashboard editor, renderers, layout persistence and Activity HTTP routes;
// only the data and socket arrivals are synthetic. No router is contacted.
func TestActivityDashboardBrowserJourney(t *testing.T) {
	if os.Getenv("MIKRODASH_ACTIVITY_BROWSER") == "" {
		t.Skip("set MIKRODASH_ACTIVITY_BROWSER=1")
	}
	s, mux, _ := activityTestServer(t)
	s.registerLayouts(mux)
	if err := s.auditDB.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: "activity-user", RoleID: "administrator", ScopeType: "router", ScopeID: "r2"}); err != nil {
		t.Fatal(err)
	}
	appendEvent := func(router, key, domain, user string) {
		t.Helper()
		now := time.Now().Add(-time.Hour).UnixMilli()
		e := activity.Event{RouterID: router, SourceKey: key, Kind: "dns", Domain: domain, User: user, IP: "198.51.100.25", Client: router + "/" + user, At: now, Received: now, TimeCertain: true}
		if err := s.auditDB.PersistActivity([]activity.Event{e}, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 12; i++ {
		user := "مستخدم تجريبي"
		if i%3 == 0 {
			user = "Synthetic User"
		}
		appendEvent("r1", strconv.Itoa(i), []string{"chatgpt.com", "facebook.com", "youtubei.googleapis.com"}[i%3], user)
	}
	appendEvent("r2", "only", "cdn.capcutapi.com", "Other Router User")
	now := time.Now().UnixMilli()
	if err := s.auditDB.PersistActivity([]activity.Event{{RouterID: "r1", SourceKey: "firewall", Kind: "firewall", Destination: "203.0.113.4:443", Protocol: "TCP", At: now, Received: now, TimeCertain: true}}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(t.TempDir(), "entry.js")
	build := exec.Command(filepath.Join(root, "web/node_modules/.bin/esbuild"), filepath.Join(root, "web/test/browser/dashboard-activity-entry.ts"), "--bundle", "--format=esm", "--outfile="+entry, "--log-level=error")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("bundle browser fixture: %v %s", err, out)
	}
	for _, prefix := range []string{"/vendor/", "/css/", "/app.css"} {
		mux.Handle("GET "+prefix, http.FileServer(http.Dir(filepath.Join(root, "web/public"))))
	}
	mux.HandleFunc("GET /fixture-entry.js", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, entry) })
	fixture := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>Dashboard Activity verification</title><link rel="stylesheet" href="/vendor/tabler.min.css"><link rel="stylesheet" href="/vendor/fonts/fonts.css"><link rel="stylesheet" href="/css/app-fonts.css"><link rel="stylesheet" href="/css/dashboard-grid.css"><link rel="stylesheet" href="/app.css"></head><body><main style="width:100%;height:100vh;display:flex;flex-direction:column"><div class="topbar">`))
		shell, _ := os.ReadFile(filepath.Join(root, "web/src/ui/shell.html"))
		sh := string(shell)
		_, _ = w.Write([]byte(sh[strings.Index(sh, `<div id="dashEditControls">`):strings.Index(sh, `<button id="themeToggle"`)]))
		_, _ = w.Write([]byte(`</div>`))
		for _, name := range []string{"dashboard", "activity"} {
			b, _ := os.ReadFile(filepath.Join(root, "web/src/ui/page-"+name+".html"))
			_, _ = w.Write(b)
		}
		_, _ = w.Write([]byte(`</main><script type="module" src="/fixture-entry.js"></script></body></html>`))
	}
	mux.HandleFunc("GET /home", fixture)
	mux.HandleFunc("GET /activity", fixture)
	mux.HandleFunc("POST /fixture/append", func(w http.ResponseWriter, r *http.Request) {
		appendEvent("r1", "appended", "chatgpt.com", "مستخدم تجريبي")
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /fixture/revoke", func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.auditDB.DeleteGrantsForScope("router", "r1"); err != nil {
			t.Fatal(err)
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	hs := httptest.NewServer(mux)
	defer hs.Close()
	cmd := exec.Command("node", filepath.Join(root, "web/test/browser/dashboard-activity-journey.cjs"), hs.URL)
	cmd.Env = append(os.Environ(), "NODE_PATH=/usr/lib/node_modules")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dashboard browser journey: %v\n%s", err, out)
	}
	t.Log(string(out))
}
