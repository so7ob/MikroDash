package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mikrodash/internal/activity"
	"mikrodash/internal/activitywire"
	"mikrodash/internal/audit"
	"mikrodash/internal/collect"
	"mikrodash/internal/geo"
	"mikrodash/internal/hub"
	"mikrodash/internal/routeros"
	"mikrodash/internal/safe"
)

type ActivityChanged struct {
	RouterID string `json:"routerId"`
	At       int64  `json:"at"`
}

var EvActivityChanged = hub.Declare[ActivityChanged]("activity:changed")

func (s *Server) registerActivity(mux *http.ServeMux) {
	for _, name := range []string{"query", "summary", "series", "domains", "clients", "events"} {
		mux.HandleFunc("GET /api/activity/"+name, s.activityQuery)
	}
	mux.HandleFunc("GET /api/activity/export", s.activityExport)
	mux.HandleFunc("GET /api/activity/settings", s.activitySettingsGet)
	mux.HandleFunc("PUT /api/activity/settings", s.activitySettingsPut)
	mux.HandleFunc("GET /api/activity/rules", s.activityRulesGet)
	mux.HandleFunc("PUT /api/activity/rules", s.activityRulesPut)
	mux.HandleFunc("DELETE /api/activity/rules", s.activityRulesDelete)
}
func (s *Server) activityAccess(w http.ResponseWriter, r *http.Request, access string) (*Session, string) {
	sess := s.layoutSession(w, r)
	if sess == nil {
		return nil, ""
	}
	id := r.URL.Query().Get("routerId")
	if id == "" || len(id) > 128 {
		writeJSON400OK(w)
		return nil, ""
	}
	if !permitted(s.rbac.CanPage(s.userIDFor(sess.Username), "activity", access, id)) {
		writeJSONErr(w, 403, "Not permitted")
		return nil, ""
	}
	if s.auditDB == nil || s.activityWire == nil {
		writeJSONErr(w, 503, "Activity storage unavailable")
		return nil, ""
	}
	if access == "write" {
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				writeJSONErr(w, 403, "Not permitted")
				return nil, ""
			}
		}
	}
	return sess, id
}
func activityFilter(r *http.Request, now time.Time) (activity.Filter, error) {
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("days"))
	if days != 7 && days != 30 {
		days = 1
	}
	f := activity.Filter{To: now.UnixMilli() + 1, Search: q.Get("q"), Client: q.Get("client"), Domain: q.Get("domain"), Service: q.Get("service"), Category: q.Get("category"), Country: q.Get("country"), Day: q.Get("day"), Hour: q.Get("hour"), Kind: q.Get("kind"), View: q.Get("view"), Group: q.Get("group"), Sort: q.Get("sort"), Desc: q.Get("dir") != "asc", Page: 1, Limit: 50}
	f.From = f.To - int64(time.Duration(days)*24*time.Hour/time.Millisecond)
	var err error
	if q.Has("from") {
		f.From, err = strconv.ParseInt(q.Get("from"), 10, 64)
		if err != nil {
			return f, err
		}
	}
	if q.Has("to") {
		f.To, err = strconv.ParseInt(q.Get("to"), 10, 64)
		if err != nil {
			return f, err
		}
	}
	if f.From >= f.To || f.To-f.From > int64(activity.Retention/time.Millisecond) || f.To > now.Add(time.Minute).UnixMilli() || f.From < now.Add(-activity.Retention-time.Minute).UnixMilli() {
		return f, errors.New("invalid time range")
	}
	if n, e := strconv.Atoi(q.Get("page")); e == nil && n > 0 && n <= 1000000 {
		f.Page = n
	}
	if n, e := strconv.Atoi(q.Get("limit")); e == nil && n > 0 && n <= 200 {
		f.Limit = n
	}
	for _, v := range []string{f.Search, f.Client, f.Domain, f.Service, f.Category, f.Country, f.Day, f.Hour} {
		if len(v) > 1024 {
			return f, errors.New("filter too long")
		}
	}
	if f.Kind != "" && f.Kind != "dns" && f.Kind != "firewall" {
		return f, errors.New("invalid kind")
	}
	if name := strings.TrimPrefix(r.URL.Path, "/api/activity/"); name != "query" && name != "export" {
		f.View = name
	}
	switch f.View {
	case "domains", "clients", "events", "rules", "summary", "series":
	default:
		f.View = "summary"
	}
	return f, nil
}
func (s *Server) activityResult(r *http.Request, id string) (activity.Result, error) {
	f, err := activityFilter(r, time.Now())
	if err != nil {
		return activity.Result{}, err
	}
	cfg, err := s.auditDB.ActivityState(id)
	if err != nil {
		return activity.Result{}, err
	}
	rules, err := s.auditDB.ActivityRules(id)
	if err != nil {
		return activity.Result{}, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	out, err := activity.Analyze(f, rules, cfg.Timezone, func(from, to int64, fn func(activity.Event) error) error {
		return s.auditDB.WalkActivity(ctx, id, from, to, fn)
	}, activityLookup())
	if err != nil {
		return out, err
	}
	if f.View == "events" {
		out.Events, out.TotalRows, err = s.auditDB.ActivityEventPage(ctx, id, f, rules, activityLookup())
		if err != nil {
			return out, err
		}
	}
	out.RouterID = id
	out.Settings = cfg.Settings
	out.Status = s.activityWire.Status(id)
	// A disabled router after restart still has historical counters in SQLite.
	if out.Status.LastPersisted == 0 {
		out.Status.Invalid = cfg.Invalid
		out.Status.Dropped = cfg.Dropped
		out.Status.LastReceived = cfg.LastReceived
		out.Status.LastPersisted = cfg.LastPersisted
	}
	out.Status.Enabled = cfg.Enabled
	out.Rules = append(rules, activity.Builtins...)
	out.Coverage, err = s.auditDB.ActivityCoverage(id, f.From-(f.To-f.From), f.To)
	if err != nil {
		return out, err
	}
	out.Comparison = out.Status.Dropped == 0 && activity.FullyCovered(out.Coverage, f.From-(f.To-f.From), f.From) && activity.FullyCovered(out.Coverage, f.From, f.To)
	return out, nil
}
func (s *Server) activityQuery(w http.ResponseWriter, r *http.Request) {
	sess, id := s.activityAccess(w, r, "read")
	if sess == nil {
		return
	}
	if _, err := activityFilter(r, time.Now()); err != nil {
		writeJSONErr(w, 400, "Invalid activity filters")
		return
	}
	out, err := s.activityResult(r, id)
	if err != nil {
		writeJSONErr(w, 500, "Activity query failed")
		return
	}
	out.CanWrite = permitted(s.rbac.CanPage(s.userIDFor(sess.Username), "activity", "write", id))
	writeJSON(w, out)
}
func (s *Server) activitySettingsGet(w http.ResponseWriter, r *http.Request) {
	sess, id := s.activityAccess(w, r, "read")
	if sess == nil {
		return
	}
	cfg, err := s.auditDB.ActivityState(id)
	if err != nil {
		writeJSON500OK(w)
		return
	}
	writeJSON(w, cfg)
}
func (s *Server) activitySettingsPut(w http.ResponseWriter, r *http.Request) {
	sess, id := s.activityAccess(w, r, "write")
	if sess == nil {
		return
	}
	var v activity.Settings
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&v) != nil {
		writeJSON400OK(w)
		return
	}
	if v.Folder == "" {
		v.Folder = activity.DefaultFolder
	}
	if !activity.ValidFolder(v.Folder) {
		writeJSONErr(w, 400, "Invalid log folder")
		return
	}
	if v.Enabled {
		if _, err := time.LoadLocation(v.Timezone); err != nil || v.Timezone == "" {
			writeJSONErr(w, 400, "Router log timezone required")
			return
		}
	}
	if len(v.Timezone) > 128 {
		writeJSON400OK(w)
		return
	}
	if s.routerRecord(id) == nil {
		writeJSONErr(w, 404, "Router unavailable")
		return
	}
	if err := s.auditDB.SetActivitySettings(id, v); err != nil {
		writeJSON500OK(w)
		return
	}
	s.activityWire.Configure(id, v)
	s.syncFleetHolds()
	s.httpRecorder(r, sess).Record(audit.Event{Action: "activity.settings", TargetType: "activity", RouterID: id})
	s.broadcastActivity(id)
	writeJSON(w, map[string]any{"ok": true})
}
func (s *Server) activityRulesGet(w http.ResponseWriter, r *http.Request) {
	sess, id := s.activityAccess(w, r, "read")
	if sess == nil {
		return
	}
	rules, err := s.auditDB.ActivityRules(id)
	if err != nil {
		writeJSON500OK(w)
		return
	}
	writeJSON(w, append(rules, activity.Builtins...))
}
func (s *Server) activityRulesPut(w http.ResponseWriter, r *http.Request) {
	sess, id := s.activityAccess(w, r, "write")
	if sess == nil {
		return
	}
	var v activity.Rule
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&v) != nil {
		writeJSON400OK(w)
		return
	}
	v, err := activity.ValidateRule(v)
	if err != nil {
		writeJSONErr(w, 400, safe.Message(err.Error()))
		return
	}
	if err = s.auditDB.SetActivityRule(id, v); err != nil {
		writeJSON500OK(w)
		return
	}
	s.httpRecorder(r, sess).Record(audit.Event{Action: "activity.rule.save", TargetType: "activity-rule", TargetName: v.Domain, RouterID: id})
	s.broadcastActivity(id)
	writeJSON(w, map[string]any{"ok": true})
}
func (s *Server) activityRulesDelete(w http.ResponseWriter, r *http.Request) {
	sess, id := s.activityAccess(w, r, "write")
	if sess == nil {
		return
	}
	domain := activity.NormalizeDomain(r.URL.Query().Get("domain"))
	if domain == "" {
		writeJSON400OK(w)
		return
	}
	if err := s.auditDB.DeleteActivityRule(id, domain); err != nil {
		writeJSON500OK(w)
		return
	}
	s.httpRecorder(r, sess).Record(audit.Event{Action: "activity.rule.remove", TargetType: "activity-rule", TargetName: domain, RouterID: id})
	s.broadcastActivity(id)
	writeJSON(w, map[string]any{"ok": true})
}
func csvSafe(v string) string {
	t := strings.TrimLeft(v, " \t\r\n")
	if t != "" && strings.ContainsRune("=+-@", rune(t[0])) {
		return "'" + v
	}
	return v
}
func (s *Server) activityExport(w http.ResponseWriter, r *http.Request) {
	sess, id := s.activityAccess(w, r, "read")
	if sess == nil {
		return
	}
	f, err := activityFilter(r, time.Now())
	if err != nil {
		writeJSONErr(w, 400, "Invalid activity filters")
		return
	}
	rules, err := s.auditDB.ActivityRules(id)
	if err != nil {
		writeJSON500OK(w)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="activity.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("\xef\xbb\xbf"))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"time_utc", "router_time", "time_certain", "kind", "domain", "service", "category", "user", "server", "ip", "mac", "qtype", "match", "protocol", "destination", "in", "out", "packet_length", "country", "city"})
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	cfg, _ := s.auditDB.ActivityState(id)
	lookup := activityLookup()
	nextAuth := time.Time{}
	err = s.auditDB.WalkActivity(ctx, id, f.From, f.To, func(e activity.Event) error {
		if e.Kind == "dns" {
			e.Service, e.Category = activity.Classify(e.Domain, rules)
		}
		activity.Enrich(&e, cfg.Timezone, lookup)
		if !activity.Matches(e, f) {
			return nil
		}
		// Recheck authorization while a large export is streaming, as well as on entry.
		if time.Now().After(nextAuth) {
			if !permitted(s.rbac.CanPage(s.userIDFor(sess.Username), "activity", "read", id)) {
				return errors.New("access revoked")
			}
			nextAuth = time.Now().Add(time.Second)
		}
		row := []string{time.UnixMilli(e.At).UTC().Format(time.RFC3339), e.RouterTime, strconv.FormatBool(e.TimeCertain), e.Kind, e.Domain, e.Service, e.Category, e.User, e.Server, e.IP, e.MAC, e.QType, e.Match, e.Protocol, e.Destination, e.In, e.Out, e.Length, e.Country, e.City}
		for i := range row {
			row[i] = csvSafe(row[i])
		}
		return cw.Write(row)
	})
	cw.Flush()
	// CSV cannot turn into a JSON error after its header. Terminate rather than
	// returning a successful, silently incomplete download.
	if err != nil || cw.Error() != nil {
		panic(http.ErrAbortHandler)
	}
}
func (s *Server) broadcastActivity(id string) {
	s.connsMu.Lock()
	clients := make([]*conn, 0, len(s.conns))
	for _, c := range s.conns {
		clients = append(clients, c)
	}
	s.connsMu.Unlock()
	for _, c := range clients {
		scope := c.scope()
		if scope.sess == nil {
			continue
		}
		if permitted(s.rbac.CanPage(s.userIDFor(scope.sess.Username), "activity", "read", id)) {
			EvActivityChanged.Send(s.hub, c.c, ActivityChanged{RouterID: id, At: time.Now().UnixMilli()})
		}
	}
}
func (s *Server) activityReader(id string) activitywire.Reader {
	if !s.holdFleet || s.sessions == nil {
		return nil
	}
	rs := s.sessions.Live()[id]
	if rs == nil || !rs.Connected() {
		return nil
	}
	return activityReader{exec: func(c routeros.Cmd) ([]routeros.Reply, error) { return rs.Exec(c) }}
}

type activityReader struct {
	exec func(routeros.Cmd) ([]routeros.Reply, error)
}

func (r activityReader) Do(c routeros.Cmd) ([]routeros.Reply, error) { return r.exec(c) }

func activityLookup() collect.GeoLookup {
	d, ok := geo.Current()
	if !ok {
		return nil
	}
	return d.Lookuper()
}
