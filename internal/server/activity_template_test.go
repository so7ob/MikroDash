package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"mikrodash/internal/activity"
	"mikrodash/internal/activitywire"
	"mikrodash/internal/cfgtpl"
	"mikrodash/internal/db"
	"mikrodash/internal/routeros"
)

func activityLibraryServer(t *testing.T) (*http.ServeMux, *Server) {
	t.Helper()
	s, mux, _ := activityTestServer(t)
	if err := s.auditDB.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: "activity-user", RoleID: "administrator", ScopeType: "global"}); err != nil {
		t.Fatal(err)
	}
	s.registerConfig(mux)
	return mux, s
}

func TestActivitySetupIsAvailableThroughTheLibraryAPI(t *testing.T) {
	mux, _ := activityLibraryServer(t)
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
	mux, s := activityLibraryServer(t)
	if err := s.auditDB.SetActivitySettings("r1", activity.Settings{Folder: "usb-test/Activity", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
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
	activityEntry := filepath.Join(t.TempDir(), "activity-entry.js")
	build = exec.Command(filepath.Join(root, "web/node_modules/.bin/esbuild"), filepath.Join(root, "web/test/browser/activity-setup-entry.ts"), "--bundle", "--format=esm", "--outfile="+activityEntry, "--log-level=error")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("bundle Activity: %v %s", err, out)
	}
	mux.HandleFunc("GET /activity-entry.js", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, activityEntry) })
	mux.HandleFunc("GET /activity", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Activity setup verification</title><link rel="stylesheet" href="/vendor/fonts/fonts.css"><link rel="stylesheet" href="/css/app-fonts.css"><link rel="stylesheet" href="/vendor/tabler.min.css"><link rel="stylesheet" href="/app.css"></head><body>`))
		b, err := os.ReadFile(filepath.Join(root, "web/src/ui/page-activity.html"))
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = w.Write(b)
		_, _ = w.Write([]byte(`<script type="module" src="/activity-entry.js"></script></body></html>`))
	})
	var setupMode atomic.Int32 // 0 missing, 1 ready, 2 unreadable, 3 no library rights
	mux.HandleFunc("POST /fixture/setup", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Mode int32 `json:"mode"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil {
			http.Error(w, "fixture mode", 400)
			return
		}
		setupMode.Store(v.Mode)
		if v.Mode == 3 {
			_, _ = s.auditDB.DeleteGrantsForScope("global", "")
		} else {
			if err := s.auditDB.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: "activity-user", RoleID: "administrator", ScopeType: "global"}); err != nil {
				t.Error(err)
			}
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	setupHandler := s.activitySetupGet(func(id string) activitywire.Reader {
		mode := setupMode.Load()
		rows := activitySetupRows()
		rows["/system/logging/action/print"][1]["disk-file-name"] = "usb-test/Activity/netlog"
		if id == "r2" {
			rows["/system/logging/action/print"][1]["disk-file-name"] = "usb1-part1/Log/netlog"
		} else if mode == 0 || mode == 3 {
			rows["/system/script/print"] = nil
		}
		return activityReader{exec: func(cmd routeros.Cmd) ([]routeros.Reply, error) {
			if mode == 2 {
				return nil, errors.New("synthetic permission failure")
			}
			return rows[cmd.Path], nil
		}}
	})
	mux.HandleFunc("GET /api/routers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"routers": []map[string]string{{"id": "r1", "label": "Synthetic Router"}}})
	})
	mux.HandleFunc("GET /api/sites", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"sites": []any{}}) })
	for _, prefix := range []string{"/app.css", "/vendor/", "/css/"} {
		mux.Handle("GET "+prefix, http.FileServer(http.Dir(filepath.Join(root, "web/public"))))
	}
	mux.HandleFunc("GET /library-entry.js", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, entry) })
	renderLibrary := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Activity library verification</title><link rel="stylesheet" href="/vendor/fonts/fonts.css"><link rel="stylesheet" href="/css/app-fonts.css"><link rel="stylesheet" href="/vendor/tabler.min.css"><link rel="stylesheet" href="/app.css"></head><body>`))
		b, err := os.ReadFile(filepath.Join(root, "web/src/ui/page-config-management.html"))
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = w.Write(b)
		_, _ = w.Write([]byte(`<script type="module" src="/library-entry.js"></script></body></html>`))
	}
	mux.HandleFunc("GET /journey", renderLibrary)
	mux.HandleFunc("GET /config-management", renderLibrary)
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/activity/setup" {
			setupHandler(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	defer hs.Close()
	cmd := exec.Command("node", script, hs.URL)
	cmd.Env = append(os.Environ(), "NODE_PATH=/usr/lib/node_modules")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("library browser journey: %v\n%s", err, out)
	}
	t.Log(string(out))
}
