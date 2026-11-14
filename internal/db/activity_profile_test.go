package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"mikrodash/internal/activity"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Opt-in target diagnostics call the actual SQL paths against an existing file
// opened mode=ro. They never open/migrate the application store or prepare its
// persistent projection. Query scratch remains private to this connection.
func TestActivityReadOnlyQueryProfile(t *testing.T) {
	dir := os.Getenv("MIKRODASH_ACTIVITY_PROFILE_DIR")
	if dir == "" {
		t.Skip("set MIKRODASH_ACTIVITY_PROFILE_DIR for read-only query profiling")
	}
	path := filepath.Join(dir, "mikrodash.db")
	if info, err := os.Stat(path); err != nil || info.IsDir() || info.Size() == 0 {
		t.Fatal("existing database required")
	}
	days, err := strconv.Atoi(os.Getenv("MIKRODASH_ACTIVITY_PROFILE_DAYS"))
	if err != nil {
		days = 7
	}
	if days != 1 && days != 7 && days != 30 {
		t.Fatal("profile days must be 1, 7 or 30")
	}
	name := fmt.Sprintf("activity_profile_%d", time.Now().UnixNano())
	trace := &activitySQLProfile{t: t}
	base, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	sql.Register(name, activityProfileDriver{trace: trace, base: base.Driver()})
	dsn, err := sqliteDSN(path, "mode=ro&_pragma=busy_timeout(1000)")
	if err != nil {
		t.Fatal(err)
	}
	h, err := sql.Open(name, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.SetMaxOpenConns(1)
	var modelRouter string
	if modelDir := os.Getenv("MIKRODASH_ACTIVITY_PROFILE_MODEL_DIR"); modelDir != "" {
		model, err := sqliteDSN(filepath.Join(modelDir, "mikrodash.db"), "mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = h.Exec("ATTACH DATABASE ? AS activity_profile_model", model); err != nil {
			t.Fatal(err)
		}
		var total, shapes int64
		if err = h.QueryRow(`SELECT source_total,shapes FROM activity_profile_model.activity_profile_model`).Scan(&total, &shapes); err != nil {
			// The timestamp is intentionally not selected or reported.
			var at int64
			if err = h.QueryRow(`SELECT source_total,shapes,created_at FROM activity_profile_model.activity_profile_model`).Scan(&total, &shapes, &at); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("isolated earlier analytical projection: facts=%d shapes=%d; event payload reads still use the live readonly source; timing only", total, shapes)
		var scopes int
		if err = h.QueryRow(`SELECT COUNT(DISTINCT router_id),MIN(router_id) FROM activity_profile_model.activity_shapes`).Scan(&scopes, &modelRouter); err != nil || scopes != 1 {
			t.Fatal("one explicit projection scope required", err)
		}
		trace.model = true
	}
	d := &DB{sql: h, path: path}
	var router, zone string
	if err = h.QueryRow(`SELECT router_id FROM activity_index_state ORDER BY total DESC LIMIT 1`).Scan(&router); err != nil {
		t.Fatal(err)
	}
	if err = h.QueryRow(`SELECT timezone FROM activity_settings WHERE router_id=?`, router).Scan(&zone); err != nil {
		t.Fatal(err)
	}
	queryRouter := router
	if trace.model {
		queryRouter = modelRouter
		t.Log("projection/router namespaces mapped explicitly; aggregate/count timings and live listing timings are separate, no combined analytical result is asserted")
	}
	var total, pending, shapes, unprepared int64
	if err = h.QueryRow(`SELECT total FROM activity_index_state WHERE router_id=?`, router).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if err = h.QueryRow(`SELECT COUNT(*) FROM activity_pending WHERE router_id=?`, router).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if err = h.QueryRow(`SELECT COUNT(*),COALESCE(SUM(ready=0),0) FROM activity_shapes WHERE router_id=?`, queryRouter).Scan(&shapes, &unprepared); err != nil {
		t.Fatal(err)
	}
	if shapes == 0 {
		t.Fatal("empty or incorrectly scoped projection cannot establish performance")
	}
	t.Logf("readonly timing only: days=%d source=%d pending=%d shapes=%d unprepared=%d; no analytical completeness assertion", days, total, pending, shapes, unprepared)
	cases := strings.Split(os.Getenv("MIKRODASH_ACTIVITY_PROFILE_CASES"), ",")
	if len(cases) == 1 && cases[0] == "" {
		cases = []string{"overview"}
	}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			now := time.Now().UnixMilli()
			if value := os.Getenv("MIKRODASH_ACTIVITY_PROFILE_NOW"); value != "" {
				var err error
				now, err = strconv.ParseInt(value, 10, 64)
				if err != nil {
					t.Fatal("invalid frozen profile clock")
				}
			}
			f := activity.Filter{From: now - int64(days)*86400000, To: now + 1, View: "summary", Group: "dnsIP", Page: 1, Limit: 10, Desc: true, Parts: "overview"}
			baseKind, modifier, _ := strings.Cut(kind, "-with-")
			switch {
			case baseKind == "overview":
			case strings.HasPrefix(baseKind, "overview-by-"):
				f.Group = activityProfileGroup(t, strings.TrimPrefix(baseKind, "overview-by-"))
			case baseKind == "clients":
				f.View, f.Parts, f.Limit = "clients", "table", 50
			case baseKind == "events":
				f.View, f.Parts, f.Sort, f.Limit = "events", "table", "last", 50
			case strings.HasPrefix(baseKind, "group-"):
				f.View, f.Parts, f.Group, f.Limit = "domains", "table", activityProfileGroup(t, strings.TrimPrefix(baseKind, "group-")), 50
			default:
				t.Fatal("unsupported profile case")
			}
			activityProfileModifier(t, h, queryRouter, zone, modifier, &f)
			trace.enabled = true
			defer func() { trace.enabled = false }()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			started := time.Now()
			var result activity.Result
			if f.View == "events" {
				if trace.model {
					counts := EmptyActivityResult(f)
					if err = d.activityGroupCounts(ctx, queryRouter, f, zone, &counts); err != nil {
						t.Fatal(err)
					}
					t.Log("projection counts measured; now time the actual live event listing with a precomputed private diagnostic count cache")
					d.activityCache.put("profile:"+kind, counts, time.Now().Add(time.Minute).UnixMilli())
					trace.model = false
					defer func() { trace.model = true }()
				}
				result, err = d.activityIndexedEvents(ctx, router, f, zone, nil, nil, "profile:"+kind, time.Now().Add(time.Minute).UnixMilli())
			} else {
				cancel()
				partialTiming := false
				for stage := 0; stage < 10; stage++ {
					if d.activityJob != nil && d.activityJob.stage == 2 && os.Getenv("MIKRODASH_ACTIVITY_PROFILE_STAGES") == "last" {
						t.Log("timing-only last-phase probe: distinct totals and earlier facets are measured separately; no combined result is asserted")
						countCtx, countCancel := context.WithTimeout(t.Context(), 20*time.Second)
						countErr := h.QueryRowContext(countCtx, `SELECT COALESCE(SUM(CASE WHEN s.kind='dns' THEN q.n ELSE 0 END),0),COALESCE(SUM(CASE WHEN s.kind='firewall' THEN q.n ELSE 0 END),0) FROM activity_query_work q JOIN activity_query_shapes s ON s.id=q.shape_id WHERE q.at>=?`, d.activityJob.result.Filter.From).Scan(&d.activityJob.result.Totals.DNS, &d.activityJob.result.Totals.Firewall)
						countCancel()
						if countErr != nil {
							t.Fatal(countErr)
						}
						d.activityJob.stage = 5
					}
					stageCtx, stageCancel := context.WithTimeout(t.Context(), 20*time.Second)
					stageStart := time.Now()
					result, err = d.activitySQLAggregate(stageCtx, queryRouter, f, nil, zone, nil, activityAggregateRequest{Key: kind})
					t.Logf("request stage=%d elapsed=%s preparing=%v error=%v", stage, time.Since(stageStart), result.Preparing, err)
					if err != nil || !result.Preparing {
						ctx = stageCtx
						defer stageCancel()
						break
					}
					stageCancel()
					if d.activityJob != nil && d.activityJob.stage == 5 && os.Getenv("MIKRODASH_ACTIVITY_PROFILE_STAGES") == "first" {
						t.Log("timing-only first-phase probe: remaining phases are measured separately; no combined result is asserted")
						partialTiming = true
						break
					}
				}
				if err == nil && result.Preparing && !partialTiming {
					err = fmt.Errorf("staged profile did not finish")
				}
			}
			if err == nil && !result.Preparing && f.Parts == "table" {
				choiceRouter := queryRouter
				if modelRouter != "" {
					trace.model = true
					t.Log("filter-choice timing uses the isolated schema-48 projection")
				} else if f.View == "events" {
					choiceRouter = router
				}
				err = d.activityFilterChoices(ctx, choiceRouter, &result)
			}
			t.Logf("case=%s days=%d elapsed=%s error=%v", kind, days, time.Since(started), err)
			if err != nil {
				t.Error(err)
			}
			if baseKind == "overview" && modifier == "" && !result.Preparing && result.Totals.DNS+result.Totals.Firewall == 0 {
				t.Error("empty selected projection range cannot establish nonempty-query performance")
			}
		})
	}
}

func activityProfileGroup(t *testing.T, name string) string {
	t.Helper()
	groups := map[string]string{"service": "service", "domain": "domain", "client": "client", "category": "category", "country": "country", "dns-country": "dnsCountry", "dns-ip": "dnsIP", "canonical": "canonical", "device-name": "deviceName", "server": "server", "qtype": "qtype", "match": "match", "ip-basis": "ipBasis", "day": "day", "hour": "hour"}
	if group, ok := groups[name]; ok {
		return group
	}
	t.Fatal("unsupported profile group")
	return ""
}

func activityProfileModifier(t *testing.T, h *sql.DB, router, zone, modifier string, f *activity.Filter) {
	t.Helper()
	value := func(column string) string {
		var s string
		q := `SELECT ` + column + ` FROM activity_shapes WHERE router_id=? AND ` + column + `<>'' GROUP BY ` + column + ` ORDER BY COUNT(*) DESC LIMIT 1`
		if err := h.QueryRow(q, router).Scan(&s); err != nil {
			t.Skip("no representative source value for requested profile filter")
		}
		return s
	}
	switch modifier {
	case "":
	case "dns", "firewall":
		f.Kind = modifier
	case "search":
		f.Search = "a"
	case "unicode":
		f.Search = "جهاز"
	case "empty":
		f.Search = "__activity_no_match_109__"
	case "domain":
		f.Domain = value("domain")
	case "client":
		f.Client = value("client")
	case "service":
		f.Service = value("service")
	case "category":
		f.Category = value("category")
	case "canonical":
		f.Canonical = value("canonical")
	case "device-name":
		f.DeviceName = value("device_name")
	case "server":
		f.Server = value("server")
	case "qtype":
		f.QType = value("qtype")
	case "match":
		f.Match = value("match_method")
	case "ip-basis":
		f.IPBasis = value("ip_basis")
	case "missing-server":
		f.Server = "-"
	case "missing-ip-basis":
		f.IPBasis = "-"
	case "dns-ip", "dns-country":
		column := "ip"
		if modifier == "dns-country" {
			column = "country"
		}
		var s string
		if err := h.QueryRow(`SELECT a.`+column+` FROM activity_shape_answers a JOIN activity_shapes s ON s.id=a.shape_id WHERE s.router_id=? GROUP BY a.`+column+` ORDER BY COUNT(*) DESC LIMIT 1`, router).Scan(&s); err != nil {
			t.Skip("no representative DNS answer filter")
		}
		if modifier == "dns-ip" {
			f.DNSIP = s
		} else {
			f.DNSCountry = s
		}
	case "country":
		if err := h.QueryRow(`SELECT country FROM activity_shapes WHERE router_id=? AND kind='firewall' GROUP BY country ORDER BY COUNT(*) DESC LIMIT 1`, router).Scan(&f.Country); err != nil {
			t.Skip("no representative firewall country")
		}
	case "combined":
		f.Kind = "dns"
		f.Search = "a"
		f.Category = value("category")
	case "day", "hour":
		var at *int64
		if err := h.QueryRow(`SELECT MAX(at) FROM activity_facts WHERE router_id=? AND at>=? AND at<?`, router, f.From, f.To).Scan(&at); err != nil {
			t.Fatal(err)
		}
		if at == nil {
			t.Skip("no dated source in selected range")
		}
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		stamp := time.UnixMilli(*at).In(loc)
		if modifier == "day" {
			f.Day = stamp.Format("2006-01-02")
		} else {
			f.Hour = stamp.Format("2006-01-02 15:00")
		}
	case "custom":
		f.From += 3600013
		f.To -= 1800007
	case "deep":
		f.Page = 10000
	default:
		if !strings.HasPrefix(modifier, "sort-") {
			t.Fatal("unsupported profile modifier")
		}
		key := strings.TrimPrefix(modifier, "sort-")
		f.Desc = !strings.HasSuffix(key, "-asc")
		key = strings.TrimSuffix(key, "-asc")
		valid := map[string]bool{"label": true, "category": true, "domains": true, "devices": true, "last": true, "count": true}
		if f.View == "events" {
			valid = map[string]bool{"label": true, "last": true, "count": true, "kind": true, "user": true}
		}
		if !valid[key] {
			t.Fatal("invalid profile sort")
		}
		f.Sort = key
	}
}

type activitySQLProfile struct {
	t       *testing.T
	enabled bool
	model   bool
	mu      sync.Mutex
}

func activityProfilePhase(q string) string {
	switch {
	case strings.HasPrefix(q, "INSERT INTO activity_query_work"):
		return "aggregate-work"
	case strings.HasPrefix(q, "INSERT INTO activity_query_shapes"):
		return "compact-metadata"
	case strings.HasPrefix(q, "WITH evidence"):
		return "paged-ranking"
	case strings.HasPrefix(q, "WITH grouped"):
		return "facet-ranking"
	case strings.HasPrefix(q, "WITH matching"):
		return "histogram"
	case strings.Contains(q, "COUNT(DISTINCT z.ip)"):
		return "answer-ip-total"
	case strings.Contains(q, "SUM(groups)"):
		return "hour-group-counts"
	case strings.Contains(q, "SUM(g.groups)"):
		return "filtered-rollup-counts"
	case strings.Contains(q, "CASE WHEN s.kind='dns'"):
		return "totals"
	case strings.Contains(q, "activity_query_work"):
		return "work-or-comparison"
	default:
		return "indexed-event-or-metadata"
	}
}

func (p *activitySQLProfile) record(q string, started time.Time, rows int, err error) {
	if p.enabled {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.t.Logf("sql phase=%s rows=%d elapsed=%s error=%v", activityProfilePhase(q), rows, time.Since(started), err)
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		p.t.Logf("diagnostic Go memory: heap=%d MiB system=%d MiB collections=%d", mem.HeapAlloc>>20, mem.Sys>>20, mem.NumGC)
	}
}

type activityProfileDriver struct {
	trace *activitySQLProfile
	base  driver.Driver
}

func (d activityProfileDriver) Open(dsn string) (driver.Conn, error) {
	c, err := d.base.Open(dsn)
	if err != nil {
		return nil, err
	}
	return &activityProfileConn{Conn: c, trace: d.trace}, nil
}

type activityProfileConn struct {
	driver.Conn
	trace *activitySQLProfile
}

var activityProfileTables = regexp.MustCompile(`\b(activity_shapes|activity_shape_answers|activity_facts|activity_daily|activity_hourly|activity_hour_totals)\b`)

func (c *activityProfileConn) source(q string) string {
	if !c.trace.model {
		return q
	}
	if strings.Contains(q, `COALESCE(MAX(revision),0),COALESCE(MAX(total),0) FROM activity_index_state`) {
		return `SELECT 1,source_total FROM activity_profile_model.activity_profile_model WHERE ?<>''`
	}
	// A private projection exercises the new storage layout without migrating
	// the running service. Live Events retain their own group/fact relationships.
	liveFacts := strings.Contains(q, "activity_event_groups") || strings.HasPrefix(q, "SELECT e.id,e.payload")
	return activityProfileTables.ReplaceAllStringFunc(q, func(table string) string {
		if table == "activity_facts" && liveFacts {
			return table
		}
		return "activity_profile_model." + table
	})
}

func (c *activityProfileConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	q = c.source(q)
	if c.trace.enabled && strings.HasPrefix(q, "INSERT INTO activity_query_work") && os.Getenv("MIKRODASH_ACTIVITY_PROFILE_PLAN") == "1" {
		rows, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, "EXPLAIN QUERY PLAN "+q, args)
		if err != nil {
			return nil, err
		}
		values := make([]driver.Value, 4)
		for rows.Next(values) == nil {
			c.trace.t.Logf("aggregate execution plan: %s", values[3])
		}
		rows.Close()
	}
	started := time.Now()
	r, err := c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
	c.trace.record(q, started, 0, err)
	return r, err
}

func (c *activityProfileConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	q = c.source(q)
	started := time.Now()
	r, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
	if err != nil {
		c.trace.record(q, started, 0, err)
		return nil, err
	}
	return &activityProfileRows{Rows: r, trace: c.trace, query: q, started: started}, nil
}

type activityProfileRows struct {
	driver.Rows
	trace   *activitySQLProfile
	query   string
	started time.Time
	count   int
	closed  bool
}

func (r *activityProfileRows) Next(values []driver.Value) error {
	err := r.Rows.Next(values)
	if err == nil {
		r.count++
	} else if err != io.EOF {
		r.trace.record(r.query, r.started, r.count, err)
	}
	return err
}

func (r *activityProfileRows) Close() error {
	err := r.Rows.Close()
	if !r.closed {
		r.closed = true
		r.trace.record(r.query, r.started, r.count, err)
	}
	return err
}
