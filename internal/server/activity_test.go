package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mikrodash/internal/activity"
	"mikrodash/internal/activitywire"
	"mikrodash/internal/db"
	"mikrodash/internal/hub"
	"mikrodash/internal/rbac"
	"mikrodash/internal/routeros"
	"mikrodash/internal/session"
	"mikrodash/internal/store"
)

func activityTestServer(t *testing.T) (*Server, *http.ServeMux, string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{".secret": "synthetic-test-secret", "routers.json": routersFixture, "settings.json": `{"authMode":"modern"}`, "users.json": `[{"id":"activity-user","username":"activity-test","role":"admin"}]`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: "activity-user", RoleID: "administrator", ScopeType: "router", ScopeID: "r1"}); err != nil {
		t.Fatal(err)
	}
	a := authFor("activity-test-token", &Session{Username: "activity-test", Role: "admin", AuthMode: "modern", Pages: map[string]string{"activity": "write"}, Readable: []string{"r1"}})
	s := &Server{store: st, auditDB: d, auth: a, hub: hub.New(), conns: map[*hub.Client]*conn{}, rbac: rbac.New(d, func() []rbac.Router { return []rbac.Router{{ID: "r1"}, {ID: "r2"}} })}
	s.sessions = session.NewManager(st, s.hub)
	s.activityWire, err = activitywire.New(d, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.activityWire.Close(); s.sessions.Shutdown(); d.Close() })
	mux := http.NewServeMux()
	s.registerActivity(mux)
	return s, mux, dir
}
func activityRequest(mux http.Handler, method, path, body, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}
func TestActivityRoutesRequireScopedPermissionAndAuditWrites(t *testing.T) {
	s, mux, _ := activityTestServer(t)
	cookie := "mikrodash_sid=activity-test-token"
	for _, route := range []string{"query", "summary", "series", "domains", "clients", "events", "settings", "rules", "export"} {
		if w := activityRequest(mux, "GET", "/api/activity/"+route+"?routerId=r1", "", ""); w.Code != 401 {
			t.Fatalf("%s unauth %d", route, w.Code)
		}
		if w := activityRequest(mux, "GET", "/api/activity/"+route+"?routerId=r2", "", cookie); w.Code != 403 {
			t.Fatalf("%s cross-router %d", route, w.Code)
		}
	}
	if w := activityRequest(mux, "PUT", "/api/activity/rules?routerId=r1", `{"domain":"chatgpt.com","subdomains":true,"service":"Synthetic AI","category":"Test"}`, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	e := activity.Event{RouterID: "r1", SourceKey: "one", At: time.Now().UnixMilli(), Received: time.Now().UnixMilli(), Kind: "dns", Domain: "chatgpt.com", User: "=SYNTHETIC()", IP: "198.51.100.25", Client: `["user","s","synthetic"]`, Count: 1}
	if err := s.auditDB.PersistActivity([]activity.Event{e}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	w := activityRequest(mux, "GET", "/api/activity/query?routerId=r1&view=domains", "", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out activity.Result
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rows) != 1 || out.Rows[0].Label != "Synthetic AI" || out.Totals.DNS != 1 || !out.CanWrite {
		t.Fatalf("stored history did not reclassify: %#v", out)
	}
	w = activityRequest(mux, "GET", "/api/activity/export?routerId=r1", "", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "'=SYNTHETIC()") {
		t.Fatal("CSV formula escaping failed", w.Body.String())
	}
	if w = activityRequest(mux, "PUT", "/api/activity/settings?routerId=r1", `{"enabled":true,"timezone":"UTC","folder":"../bad"}`, cookie); w.Code != 400 {
		t.Fatal("invalid folder accepted")
	}
	if _, err := s.auditDB.DeleteGrantsForScope("router", "r1"); err != nil {
		t.Fatal(err)
	}
	if w = activityRequest(mux, "GET", "/api/activity/query?routerId=r1", "", cookie); w.Code != 403 {
		t.Fatal("revoked grant still reads activity")
	}
}
func TestActivitySettingsPersistAndBackgroundHoldRunsNoPageCollectors(t *testing.T) {
	s, mux, _ := activityTestServer(t)
	s.holdFleet = true
	w := activityRequest(mux, "PUT", "/api/activity/settings?routerId=r1", `{"enabled":true,"timezone":"UTC","folder":"usb1-part1/Log"}`, "mikrodash_sid=activity-test-token")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !hasHold(s.sessions.Held("r1"), "activity") {
		t.Fatal("save never retained background session")
	}
	cfg, err := s.auditDB.ActivityState("r1")
	if err != nil || !cfg.Enabled || cfg.Folder != activity.DefaultFolder {
		t.Fatal("source settings not persisted")
	}
	w = activityRequest(mux, "PUT", "/api/activity/settings?routerId=r1", `{"enabled":false,"timezone":"UTC","folder":"usb1-part1/Log"}`, "mikrodash_sid=activity-test-token")
	if w.Code != 200 || hasHold(s.sessions.Held("r1"), "activity") {
		t.Fatal("disabling did not drop hold")
	}
}
func TestActivityEventGroupsKeepDifferentIdentityAndPaginate(t *testing.T) {
	s, _, _ := activityTestServer(t)
	now := time.Now().UnixMilli()
	events := []activity.Event{}
	for i := 0; i < 6; i++ {
		events = append(events, activity.Event{RouterID: "r1", SourceKey: strconv.Itoa(i), At: now, Received: now, Kind: "dns", Domain: "chatgpt.com", Client: "same-user", IP: "198.51.100.25", QType: "A"})
	}
	e := events[0]
	e.SourceKey = "different-ip"
	e.IP = "198.51.100.26"
	events = append(events, e)
	if err := s.auditDB.PersistActivity(events, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	rows, total, err := s.auditDB.ActivityEventPage(context.Background(), "r1", activity.Filter{From: now - 1000, To: now + 1000, Page: 1, Limit: 1, Sort: "count", Desc: true}, nil)
	if err != nil || total != 2 || len(rows) != 1 || rows[0].Count != 6 {
		t.Fatal("event grouping/pagination", total, rows, err)
	}
}

// This opt-in browser journey drives the real page and real APIs, backed only
// by an in-memory synthetic RouterOS archive. It never reaches a real router.
func TestActivityBrowserJourney(t *testing.T) {
	script := os.Getenv("MIKRODASH_ACTIVITY_BROWSER")
	if script == "" {
		t.Skip("set MIKRODASH_ACTIVITY_BROWSER to the browser journey script")
	}
	s, mux, _ := activityTestServer(t)
	s.activityWire.Close()
	fake := &activityBrowserArchive{body: syntheticArchive()}
	var err error
	s.activityWire, err = activitywire.New(s.auditDB, func(string) activitywire.Reader { return fake }, nil)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if script == "1" {
		script = filepath.Join(root, "web/test/browser/activity-journey.cjs")
	}
	entry := filepath.Join(t.TempDir(), "activity-entry.js")
	build := exec.Command(filepath.Join(root, "web/node_modules/.bin/esbuild"), filepath.Join(root, "web/test/browser/activity-entry.ts"), "--bundle", "--format=esm", "--outfile="+entry, "--log-level=error")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("bundle browser fixture: %v %s", err, out)
	}
	mux.Handle("GET /activity", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(root, "web/dist/index.html"))
	}))
	mux.Handle("GET /app.js", http.FileServer(http.Dir(filepath.Join(root, "web/dist"))))
	mux.Handle("GET /app.css", http.FileServer(http.Dir(filepath.Join(root, "web/public"))))
	mux.Handle("GET /vendor/", http.FileServer(http.Dir(filepath.Join(root, "web/public"))))
	mux.Handle("GET /css/", http.FileServer(http.Dir(filepath.Join(root, "web/public"))))
	// The standalone entry boots the real page module without unrelated fleet UI.
	mux.HandleFunc("GET /activity-entry.js", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, entry) })
	mux.HandleFunc("GET /journey", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html lang="en"><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>Activity analytics verification</title><link rel="stylesheet" href="/vendor/fonts/fonts.css"><link rel="stylesheet" href="/css/app-fonts.css"><link rel="stylesheet" href="/vendor/tabler.min.css"><link rel="stylesheet" href="/app.css"></head><body>`))
		b, _ := os.ReadFile(filepath.Join(root, "web/src/ui/page-activity.html"))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte(`<script type="module" src="/activity-entry.js"></script></body></html>`))
	})
	hs := httptest.NewServer(mux)
	defer hs.Close()
	cmd := exec.Command("node", script, hs.URL)
	cmd.Env = append(os.Environ(), "NODE_PATH=/usr/lib/node_modules")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser journey: %v\n%s", err, out)
	}
	t.Log(string(out))
}

type activityBrowserArchive struct{ body string }

func (f *activityBrowserArchive) Do(c routeros.Cmd) ([]routeros.Reply, error) {
	if c.Path == "/file/print" {
		return []routeros.Reply{{"name": activity.DefaultFolder + "/netlog.0.txt", "size": strconv.Itoa(len(f.body)), "last-modified": "2026-10-16"}}, nil
	}
	get := func(k string) int {
		for _, a := range c.Args {
			if strings.HasPrefix(a, "="+k+"=") {
				n, _ := strconv.Atoi(strings.TrimPrefix(a, "="+k+"="))
				return n
			}
		}
		return 0
	}
	off, n := get("offset"), get("chunk-size")
	end := off + n
	if end > len(f.body) {
		end = len(f.body)
	}
	return []routeros.Reply{{"data": f.body[off:end]}}, nil
}
func syntheticArchive() string {
	var b strings.Builder
	domains := []string{"chatgpt.com", "z-gateway.facebook.com", "youtubei.googleapis.com", "cdn.capcutapi.com", "learning.example", "video.example"}
	for i := 0; i < 130; i++ {
		user := "synthetic-user"
		if i%3 == 0 {
			user = "مستخدم تجريبي"
		}
		if i%7 == 0 {
			user = "-"
		}
		domain := domains[i%len(domains)]
		if i == 129 {
			// Exercise a sub-1% selectable slice and the non-selectable Other tail.
			domain, user = "rare.example", "rare-test-user"
		}
		p := map[string]any{"Connection": map[string]string{"server": "hs-test", "qtype": "A", "match": "active-now"}, "Destination": map[string]string{"domain": domain}, "Source": map[string]string{"user": user, "ip": "198.51.100.25", "mac": "02:00:00:00:00:25"}}
		if i%3 != 0 {
			destination := p["Destination"].(map[string]string)
			// Keep DNS answer evidence distinct from a connected destination.
			p["Destination"] = map[string]any{"domain": destination["domain"], "canonical": fmt.Sprintf("edge%d.example", i%2+1), "dns_ips": []string{fmt.Sprintf("198.51.100.%d", i%60+1), fmt.Sprintf("2001:db8::%x", i%60+1), "203.0.113.8"}, "ip_basis": "dns-cache"}
			name := fmt.Sprintf("Synthetic-Device-%02d", i%12)
			if i%5 == 1 {
				name = "جهاز تجريبي"
			}
			p["Source"].(map[string]string)["device_name"] = name
			p["Connection"].(map[string]string)["match"] = "manual-mac"
			if i%4 == 1 {
				p["Connection"].(map[string]string)["qtype"] = "AAAA"
			}
		}
		j, _ := json.Marshal(p)
		b.WriteString(time.Now().UTC().Add(-time.Duration(i)*time.Minute).Format("2006-01-02 15:04:05") + " script,info DNSUSER " + string(j) + "\n")
	}
	b.WriteString(time.Now().UTC().Format("2006-01-02 15:04:05") + " firewall,info srcnat: in:bridge out:ether1, proto TCP (SYN), 198.51.100.25:1234->203.0.113.4:443, len 60\n")
	return b.String()
}

func TestActivityReadOnlyCannotChangeSourceOrClassification(t *testing.T) {
	s, mux, _ := activityTestServer(t)
	if _, err := s.auditDB.DeleteGrantsForScope("router", "r1"); err != nil {
		t.Fatal(err)
	}
	role, err := s.auditDB.CreateRole(map[string]any{"name": "Activity reader"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.auditDB.SetRolePages(role.ID, []db.RolePage{{Page: "activity", Access: "read"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.auditDB.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: "activity-user", RoleID: role.ID, ScopeType: "router", ScopeID: "r1"}); err != nil {
		t.Fatal(err)
	}
	cookie := "mikrodash_sid=activity-test-token"
	if w := activityRequest(mux, "GET", "/api/activity/query?routerId=r1", "", cookie); w.Code != 200 {
		t.Fatal("reader cannot view", w.Code)
	}
	for _, endpoint := range []string{"settings", "rules"} {
		if w := activityRequest(mux, "PUT", "/api/activity/"+endpoint+"?routerId=r1", `{}`, cookie); w.Code != 403 {
			t.Fatal("read-only wrote", endpoint, w.Code)
		}
	}
	if w := activityRequest(mux, "DELETE", "/api/activity/rules?routerId=r1&domain=chatgpt.com", "", cookie); w.Code != 403 {
		t.Fatal("read-only removed rule")
	}
}

func TestActivityMetadataAPIFiltersCSVAndScope(t *testing.T) {
	s, mux, _ := activityTestServer(t)
	now := time.Now().Add(-time.Second).UnixMilli()
	e := activity.Event{RouterID: "r1", SourceKey: "metadata", At: now, Received: now, Kind: "dns", Domain: "chatgpt.com", User: "Synthetic PC", Server: "hs-test", Client: "historical", IP: "198.51.100.25", MAC: "02:00:00:00:00:25", QType: "A", Match: "manual-mac", Canonical: "cdn.example", DeviceName: "=SUM(1,2)", IPBasis: "=dns-cache", DNSIPs: []string{"198.51.100.7", "2001:db8::7"}}
	legacy := activity.Event{RouterID: "r1", SourceKey: "legacy", At: now - 1000, Received: now, Kind: "dns", Domain: "legacy.example"}
	other := e
	other.RouterID = "r2"
	other.SourceKey = "other"
	other.DeviceName = "Other router device"
	if err := s.auditDB.PersistActivity([]activity.Event{e, legacy, other}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	cookie := "mikrodash_sid=activity-test-token"
	for key, value := range map[string]string{"canonical": e.Canonical, "deviceName": e.DeviceName, "dnsIP": "2001:0db8::7", "ipBasis": e.IPBasis, "server": e.Server, "qtype": "A", "match": "manual-mac", "q": "2001:db8::7", "dnsCountry": "Unknown"} {
		q := url.Values{"routerId": {"r1"}, "view": {"events"}, key: {value}}
		w := activityRequest(mux, "GET", "/api/activity/query?"+q.Encode(), "", cookie)
		var result activity.Result
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Events) != 1 || result.Events[0].DeviceName != e.DeviceName || len(result.Events[0].DNSIPs) != 2 || len(result.Events[0].DNSAnswers) != 2 || result.Events[0].DNSAnswers[1].Country != "Unknown" || result.Events[0].Canonical != e.Canonical || result.Events[0].IPBasis != e.IPBasis || result.Totals.DNS != 1 || result.Events[0].Country != "Unknown" {
			t.Fatal("metadata route/filter lost fields or scope", key, w.Code, w.Body.String())
		}
	}
	w := activityRequest(mux, "GET", "/api/activity/query?routerId=r1&evidenceGroup=dnsIP&limit=1", "", cookie)
	var result activity.Result
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.TotalRows != 2 || len(result.Rows) != 1 || result.Totals.Answered != 1 || result.Totals.AnswerIPs != 2 || result.Totals.DeviceNames != 1 || result.Totals.Canonicals != 1 || result.GeoAvailable || len(result.DNSCountries) != 1 || result.DNSCountries[0].Key != "Unknown" || result.DNSCountries[0].Count != 2 || len(result.Countries) != 0 {
		t.Fatal("overview evidence pagination/units", w.Code, w.Body.String())
	}
	if w := activityRequest(mux, "GET", "/api/activity/query?routerId=r1&dnsIP=invalid", "", cookie); w.Code != 400 {
		t.Fatal("invalid address filter accepted", w.Code)
	}
	if w := activityRequest(mux, "GET", "/api/activity/query?routerId=r2&deviceName=secret", "", cookie); w.Code != 403 {
		t.Fatal("metadata crossed router authorization", w.Code)
	}
	w = activityRequest(mux, "GET", "/api/activity/export?routerId=r1&dnsCountry=Unknown&dnsIP=2001%3Adb8%3A%3A7", "", cookie)
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if w.Code != 200 || err != nil || len(records) != 2 {
		t.Fatal("metadata CSV export", w.Code, err, records)
	}
	values := map[string]string{}
	for i, key := range records[0] {
		values[key] = records[1][i]
	}
	if values["canonical"] != "cdn.example" || values["dns_ips"] != "198.51.100.7 | 2001:db8::7" || values["device_name"] != "'=SUM(1,2)" || values["ip_basis"] != "'=dns-cache" || !strings.Contains(values["dns_answer_locations"], `"ip":"2001:db8::7","country":"Unknown"`) {
		t.Fatal("metadata omitted or unsafe in CSV", values)
	}
}
