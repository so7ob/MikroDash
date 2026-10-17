package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mikrodash/internal/cfgtpl"
	"mikrodash/internal/db"
)

func activityLibraryServer(t *testing.T) *http.ServeMux {
	t.Helper()
	s, mux, _ := activityTestServer(t)
	if err := s.auditDB.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: "activity-user", RoleID: "administrator", ScopeType: "global"}); err != nil {
		t.Fatal(err)
	}
	s.registerConfig(mux)
	return mux
}

func TestActivitySetupIsAvailableThroughTheLibraryAPI(t *testing.T) {
	mux := activityLibraryServer(t)
	cookie := "mikrodash_sid=activity-test-token"
	path := cfgPrefix + "templates/canned:activity-logging"
	if w := activityRequest(mux, "GET", path, "", ""); w.Code != 401 {
		t.Fatal("anonymous library read", w.Code)
	}
	w := activityRequest(mux, "GET", cfgPrefix+"templates", "", cookie)
	var list struct {
		Canned []cfgtpl.Canned `json:"canned"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil {
		t.Fatal("library list", w.Code, w.Body.String())
	}
	found := 0
	for _, c := range list.Canned {
		if c.ID == "activity-logging" && c.Category == "monitoring" {
			found++
		}
	}
	if found != 1 {
		t.Fatal("Activity must appear once in Monitoring", found)
	}
	w = activityRequest(mux, "GET", path, "", cookie)
	var reply struct {
		Template cfgView `json:"template"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
		t.Fatal("template detail", w.Code, w.Body.String())
	}
	if reply.Template.ID != "canned:activity-logging" || cfgtpl.Worst(reply.Template.Findings) != cfgtpl.Ack {
		t.Fatal("library lost shipped script acknowledgements")
	}
	for _, want := range []string{"Attach USB", "directory MUST exist", "DNSUserLine", "DNSJoin", "NetArchive", "Synchronization", "30 days"} {
		if !strings.Contains(reply.Template.Body, want) {
			t.Errorf("template preview omits %s", want)
		}
	}
	var defs []cfgtpl.VarDef
	if err := json.Unmarshal([]byte(reply.Template.Variables), &defs); err != nil {
		t.Fatal(err)
	}
	if len(defs) != 3 || defs[0].Default != "usb1-part1/Log/netlog" {
		t.Fatal("USB settings missing from template detail")
	}
	// No settings or event import starts merely by reading this setup template.
	settings := activityRequest(mux, "GET", "/api/activity/settings?routerId=r1", "", cookie)
	if settings.Code != http.StatusOK {
		t.Fatal("Activity settings read failed", settings.Code)
	}
	if strings.Contains(settings.Body.String(), `"enabled":true`) {
		t.Fatal("reading the library enabled collection")
	}
}

// Open the real library module against real template APIs in an isolated
// synthetic store. There are deliberately no preview/deploy routes to hardware.
func TestActivityLibraryBrowserJourney(t *testing.T) {
	script := os.Getenv("MIKRODASH_ACTIVITY_LIBRARY_BROWSER")
	if script == "" {
		t.Skip("set MIKRODASH_ACTIVITY_LIBRARY_BROWSER for the library journey")
	}
	mux := activityLibraryServer(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if script == "1" {
		script = filepath.Join(root, "web/test/browser/activity-library-journey.cjs")
	}
	entry := filepath.Join(t.TempDir(), "library-entry.js")
	build := exec.Command(filepath.Join(root, "web/node_modules/.bin/esbuild"), filepath.Join(root, "web/test/browser/activity-library-entry.ts"), "--bundle", "--format=esm", "--outfile="+entry, "--log-level=error")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("bundle library: %v %s", err, out)
	}
	mux.HandleFunc("GET /api/routers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"routers": []map[string]string{{"id": "r1", "label": "Synthetic Router"}}})
	})
	mux.HandleFunc("GET /api/sites", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"sites": []any{}}) })
	for _, prefix := range []string{"/app.css", "/vendor/", "/css/"} {
		mux.Handle("GET "+prefix, http.FileServer(http.Dir(filepath.Join(root, "web/public"))))
	}
	mux.HandleFunc("GET /library-entry.js", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, entry) })
	mux.HandleFunc("GET /journey", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Activity library verification</title><link rel="stylesheet" href="/vendor/fonts/fonts.css"><link rel="stylesheet" href="/css/app-fonts.css"><link rel="stylesheet" href="/vendor/tabler.min.css"><link rel="stylesheet" href="/app.css"></head><body>`))
		b, err := os.ReadFile(filepath.Join(root, "web/src/ui/page-config-management.html"))
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = w.Write(b)
		_, _ = w.Write([]byte(`<script type="module" src="/library-entry.js"></script></body></html>`))
	})
	hs := httptest.NewServer(mux)
	defer hs.Close()
	cmd := exec.Command("node", script, hs.URL)
	cmd.Env = append(os.Environ(), "NODE_PATH=/usr/lib/node_modules")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("library browser journey: %v\n%s", err, out)
	}
	t.Log(string(out))
}
