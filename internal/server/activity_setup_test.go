package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"mikrodash/internal/activitywire"
	"mikrodash/internal/db"
	"mikrodash/internal/routeros"
)

func activitySetupRows() map[string][]routeros.Reply {
	return map[string][]routeros.Reply{
		"/system/script/print": {{"name": "DNSUserLine", "policy": "read,write,test", "dont-require-permissions": "true"}},
		"/system/logging/action/print": {
			{"name": "DNSJoin", "target": "script", "script": "DNSUserLine"},
			{"name": "NetArchive", "target": "disk", "disk-file-name": "usb1-part1/Log/netlog"},
		},
		"/system/logging/print": {
			{"action": "DNSJoin", "topics": "dns", "regex": "query from [0-9.]+: #", "disabled": "false"},
			{"action": "NetArchive", "topics": "script", "regex": "^DNSUSER"},
			{"action": "NetArchive", "topics": "script,info", "regex": "^HS_"},
		},
	}
}

func TestActivitySetupChecksConfigurationRatherThanEventCounts(t *testing.T) {
	cases := []struct {
		name, key string
		change    func(map[string][]routeros.Reply)
	}{
		{"invalid script", "script", func(r map[string][]routeros.Reply) { r["/system/script/print"][0]["invalid"] = "true" }},
		{"missing read policy", "script", func(r map[string][]routeros.Reply) { r["/system/script/print"][0]["policy"] = "write,test" }},
		{"hook permissions", "script", func(r map[string][]routeros.Reply) {
			r["/system/script/print"][0]["dont-require-permissions"] = "false"
		}},
		{"wrong script", "join", func(r map[string][]routeros.Reply) { r["/system/logging/action/print"][0]["script"] = "other" }},
		{"memory archive", "archive", func(r map[string][]routeros.Reply) { r["/system/logging/action/print"][1]["target"] = "memory" }},
		{"other directory", "archive", func(r map[string][]routeros.Reply) {
			r["/system/logging/action/print"][1]["disk-file-name"] = "usb1-part1/Log-other/netlog"
		}},
		{"disabled query rule", "dns-rule", func(r map[string][]routeros.Reply) { r["/system/logging/print"][0]["disabled"] = "true" }},
		{"bad regex", "dns-rule", func(r map[string][]routeros.Reply) { r["/system/logging/print"][0]["regex"] = "[" }},
		{"excluded info", "dnsuser-rule", func(r map[string][]routeros.Reply) { r["/system/logging/print"][1]["topics"] = "script,!info" }},
		{"wrong archive pattern", "dnsuser-rule", func(r map[string][]routeros.Reply) { r["/system/logging/print"][1]["regex"] = "^HS_" }},
		{"optional HotSpot hook", "hotspot-rule", func(r map[string][]routeros.Reply) { r["/system/logging/print"] = r["/system/logging/print"][:2] }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := activitySetupRows()
			c.change(r)
			checks := activitySetupChecks(r["/system/script/print"], r["/system/logging/action/print"], r["/system/logging/print"], "usb1-part1/Log")
			if len(checks) != 6 {
				t.Fatal("no setup checks measured")
			}
			for _, check := range checks {
				if check.Ready == (check.Key == c.key) {
					t.Errorf("%s readiness %v", check.Key, check.Ready)
				}
			}
		})
	}
	// Topic order, negations and anchored/custom filters may describe the same
	// working pipeline. Comparing regex/topic strings would produce a false CTA.
	r := activitySetupRows()
	r["/system/logging/print"][1]["topics"] = "info,script,!error"
	r["/system/logging/print"][1]["regex"] = "^DNSUSER "
	for _, c := range activitySetupChecks(r["/system/script/print"], r["/system/logging/action/print"], r["/system/logging/print"], "usb1-part1/Log") {
		if !c.Ready {
			t.Errorf("working variant rejected: %s", c.Key)
		}
	}
}

func TestActivitySetupEndpointIsReadOnlyScopedAndDistinguishesUnknown(t *testing.T) {
	s, mux, _ := activityTestServer(t)
	rows := activitySetupRows()
	calls := 0
	fail := false
	revoke := false
	reader := activityReader{exec: func(cmd routeros.Cmd) ([]routeros.Reply, error) {
		calls++
		if !strings.HasSuffix(cmd.Path, "/print") || cmd.Timeout <= 0 {
			t.Fatal("unbounded or non-read command", cmd.Path)
		}
		for _, a := range cmd.Args {
			if strings.Contains(a, "source") || strings.Contains(a, "password") {
				t.Fatal("setup reads code or credentials")
			}
		}
		if fail {
			return nil, errors.New("synthetic credential and address must never reach browser")
		}
		if revoke && calls%3 == 0 {
			if _, err := s.auditDB.DeleteGrantsForScope("router", "r1"); err != nil {
				t.Fatal(err)
			}
		}
		return rows[cmd.Path], nil
	}}
	custom := http.NewServeMux()
	custom.HandleFunc("GET /api/activity/setup", s.activitySetupGet(func(id string) activitywire.Reader {
		if id != "r1" {
			t.Fatal("other router read")
		}
		return reader
	}))
	cookie := "mikrodash_sid=activity-test-token"
	for _, c := range []struct {
		path, cookie string
		code         int
	}{{"r1", "", 401}, {"r2", cookie, 403}} {
		if w := activityRequest(custom, "GET", "/api/activity/setup?routerId="+c.path, "", c.cookie); w.Code != c.code {
			t.Fatal(w.Code)
		}
	}
	if calls != 0 {
		t.Fatal("unauthorized router read")
	}
	check := func(handler http.Handler) activitySetupReport {
		t.Helper()
		w := activityRequest(handler, "GET", "/api/activity/setup?routerId=r1", "", cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var out activitySetupReport
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Checks == nil || out.RouterID != "r1" {
			t.Fatal("incomplete report")
		}
		return out
	}
	out := check(custom)
	if out.State != "ready" || out.CanConfigure || out.TemplateURL != "" || calls != 3 {
		t.Fatalf("read-only viewer: %+v reads=%d", out, calls)
	}
	if err := s.auditDB.UpsertGrant(db.GrantSpec{PrincipalType: "user", PrincipalID: "activity-user", RoleID: "administrator", ScopeType: "global"}); err != nil {
		t.Fatal(err)
	}
	rows["/system/script/print"] = nil
	out = check(custom)
	if out.State != "missing" || !out.CanConfigure {
		t.Fatal("missing setup was not actionable for admin")
	}
	u, err := url.Parse(out.TemplateURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/config-management" || u.Query().Get("routerId") != "r1" || u.Query().Get("template") != "canned:activity-logging" || u.Query().Get("archivePath") != "usb1-part1/Log/netlog" {
		t.Fatal("wrong template link", out.TemplateURL)
	}
	rows = activitySetupRows()
	rows["/system/logging/print"] = rows["/system/logging/print"][:2]
	if out = check(custom); out.State != "ready" {
		t.Fatal("optional HS_ rule blocked DNS analytics")
	}
	fail = true
	out = check(custom)
	if out.State != "unknown" || len(out.Checks) != 0 || out.TemplateURL != "" || strings.Contains(out.Error, "synthetic") {
		t.Fatal("failed read became missing setup or leaked a trap")
	}
	// The registered production endpoint cannot manufacture missing settings
	// when there is no shared live connection, and must not dial one itself.
	out = check(mux)
	if out.State != "unknown" || len(s.sessions.Live()) != 0 {
		t.Fatal("offline setup check dialled or claimed absence")
	}
	fail = false
	revoke = true
	if _, err := s.auditDB.DeleteGrantsForScope("global", ""); err != nil {
		t.Fatal(err)
	}
	// Align the spy's three-read cycle before revocation.
	calls = 0
	if w := activityRequest(custom, "GET", "/api/activity/setup?routerId=r1", "", cookie); w.Code != 403 {
		t.Fatal("revoked access returned setup details", w.Code)
	}
}
