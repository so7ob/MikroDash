package server

import (
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"mikrodash/internal/activitywire"
	"mikrodash/internal/routeros"
)

type activitySetupCheck struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Ready    bool   `json:"ready"`
	Required bool   `json:"required"`
}

type activitySetupReport struct {
	RouterID     string               `json:"routerId"`
	State        string               `json:"state"` // ready, missing, unknown; a failed read is never missing
	CheckedAt    int64                `json:"checkedAt"`
	Checks       []activitySetupCheck `json:"checks"`
	CanConfigure bool                 `json:"canConfigure"`
	TemplateURL  string               `json:"templateUrl,omitempty"`
	Error        string               `json:"error,omitempty"`
}

// On-demand, through the existing shared connection. This never dials, takes
// a collector hold, reads script source or writes configuration. The factory
// makes the same authorized handler usable with a synthetic reader in tests.
func (s *Server) activitySetupGet(readerFor func(string) activitywire.Reader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, id := s.activityAccess(w, r, "read")
		if sess == nil {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		cfg, err := s.auditDB.ActivityState(id)
		if err != nil {
			writeJSON500OK(w)
			return
		}
		out := activitySetupReport{RouterID: id, State: "unknown", CheckedAt: time.Now().UnixMilli(), Checks: []activitySetupCheck{}}
		reader := readerFor(id)
		if reader == nil {
			out.Error = "Router setup cannot be checked while its shared connection is unavailable."
			writeJSON(w, out)
			return
		}
		menus := []struct{ path, props string }{
			{"/system/script/print", "name,invalid,policy,dont-require-permissions"},
			{"/system/logging/action/print", "name,target,script,disk-file-name"},
			{"/system/logging/print", "action,topics,regex,disabled,prefix"},
		}
		rows := make([][]routeros.Reply, len(menus))
		for i, menu := range menus {
			if r.Context().Err() != nil {
				return
			}
			rows[i], err = reader.Do(routeros.Cmd{Path: menu.path, Args: []string{"=.proplist=" + menu.props}, Timeout: 3 * time.Second})
			if err != nil {
				// Nothing from an arbitrary trap (addresses, names or credentials) is
				// needed for this verdict, and a partial read cannot diagnose absence.
				out.Error = "Router setup could not be read. Check the connection and API read permissions."
				writeJSON(w, out)
				return
			}
		}
		// Permissions may have changed while the router answered.
		if !permitted(s.rbac.CanPage(s.userIDFor(sess.Username), "activity", "read", id)) {
			writeJSONErr(w, 403, "Not permitted")
			return
		}
		out.CanConfigure = cfgMayChange(sess, s.isGlobalAdmin(sess)) && permitted(s.rbac.CanPage(s.userIDFor(sess.Username), "config-management", "write", id))
		if out.CanConfigure {
			q := url.Values{"template": {"canned:activity-logging"}, "routerId": {id}, "archivePath": {cfg.Folder + "/netlog"}}
			out.TemplateURL = "/config-management?" + q.Encode()
		}
		out.Checks = activitySetupChecks(rows[0], rows[1], rows[2], cfg.Folder)
		out.State = "ready"
		for _, check := range out.Checks {
			if check.Required && !check.Ready {
				out.State = "missing"
			}
		}
		out.CheckedAt = time.Now().UnixMilli()
		writeJSON(w, out)
	}
}

// Check only evidence of logging configuration. Existing event counts do not
// prove the router is still configured. Optional HS_ hooks are not required
// for DNS analytics. Patterns are tested on the messages the setup consumes,
// accepting working anchored/custom filters instead of comparing their text.
func activitySetupChecks(scripts, actions, rules []routeros.Reply, folder string) []activitySetupCheck {
	truth := func(v string) bool { return v == "true" || v == "yes" }
	script, join, disk := false, false, false
	for _, row := range scripts {
		if row["name"] != "DNSUserLine" || truth(row["invalid"]) {
			continue
		}
		policies := map[string]bool{}
		for _, p := range strings.Split(row["policy"], ",") {
			policies[p] = true
		}
		script = policies["read"] && policies["write"] && policies["test"] && truth(row["dont-require-permissions"])
	}
	for _, row := range actions {
		if row["name"] == "DNSJoin" && row["target"] == "script" && row["script"] == "DNSUserLine" {
			join = true
		}
		if row["name"] == "NetArchive" && row["target"] == "disk" && path.Dir(row["disk-file-name"]) == folder {
			disk = true
		}
	}
	matches := func(action, topics, message string) bool {
		for _, row := range rules {
			if row["action"] != action || truth(row["disabled"]) || row["prefix"] != "" {
				continue
			}
			available := "," + topics + ","
			ok := true
			for _, topic := range strings.Split(row["topics"], ",") {
				topic = strings.TrimSpace(topic)
				neg := strings.HasPrefix(topic, "!")
				topic = strings.TrimPrefix(topic, "!")
				present := strings.Contains(available, ","+topic+",")
				if topic == "" || present == neg {
					ok = false
				}
			}
			re, err := regexp.Compile(row["regex"])
			if ok && err == nil && re.MatchString(message) {
				return true
			}
		}
		return false
	}
	return []activitySetupCheck{
		{"script", "DNSUserLine script and hook permissions", script, true},
		{"join", "DNSJoin script action", join, true},
		{"archive", "NetArchive disk action in the selected log folder", disk, true},
		{"dns-rule", "Enabled DNS query rule to DNSJoin", matches("DNSJoin", "dns,debug", "query from 198.51.100.25: #42 example.com A"), true},
		{"dnsuser-rule", "Enabled DNSUSER archive rule", matches("NetArchive", "script,info", "DNSUSER {}"), true},
		{"hotspot-rule", "HS_ archive rule (optional)", matches("NetArchive", "script,info", "HS_LOGIN synthetic"), false},
	}
}
