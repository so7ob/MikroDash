package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"mikrodash/internal/activity"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// Compare optimized paths with the independent source-payload analyzer. Seed
// outside every requested range too, so an empty week/month cannot pass the test.
func TestActivityAllRangesMatchSourceAnalysis(t *testing.T) {
	d, now, rules, lookup := indexedActivityFixture(t)
	var original []activity.Event
	if err := d.WalkActivity(t.Context(), "r1", now-72*3600000, now+1000, func(e activity.Event) error {
		original = append(original, e)
		return nil
	}); err != nil || len(original) == 0 {
		t.Fatal("nonempty source fixture required", err)
	}
	var events []activity.Event
	for _, day := range []int{4, 7, 14, 28, 30, 32} {
		for _, source := range original {
			e := source
			e.ID = 0
			e.SourceKey = fmt.Sprintf("range:%d:%s", day, e.SourceKey)
			e.At -= int64(day) * 86400000
			events = append(events, e)
		}
	}
	events = append(events, activity.Event{RouterID: "r2", SourceKey: "other-router", At: now, Received: now, Kind: "dns", Domain: "must-not-leak.example"})
	if err := d.PersistActivity(events, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	type queryCase struct {
		name string
		edit func(*activity.Filter)
	}
	queries := []queryCase{
		{"full", func(f *activity.Filter) {}},
		{"overview", func(f *activity.Filter) { f.Parts = "overview" }},
		{"clients", func(f *activity.Filter) { f.View, f.Parts = "clients", "table" }},
		{"series", func(f *activity.Filter) { f.View = "series" }},
		{"dns", func(f *activity.Filter) { f.Kind = "dns" }},
		{"firewall", func(f *activity.Filter) { f.Kind = "firewall" }},
		{"search-unicode-literal", func(f *activity.Filter) { f.Search = "جهاز %_ aa" }},
		{"empty", func(f *activity.Filter) { f.Search = "__missing_range_109__" }},
		{"domain", func(f *activity.Filter) { f.Domain = "d1.example" }},
		{"client", func(f *activity.Filter) { f.Client = original[1].Client }},
		{"service", func(f *activity.Filter) { f.Service = "Service خاص" }},
		{"category", func(f *activity.Filter) { f.Category = "Research" }},
		{"canonical", func(f *activity.Filter) { f.Canonical = "أمثلة.example" }},
		{"device", func(f *activity.Filter) { f.DeviceName = "جهاز %_ Aa" }},
		{"dns-ip", func(f *activity.Filter) { f.DNSIP = "2001:db8::8" }},
		{"dns-country", func(f *activity.Filter) { f.DNSCountry = "DE" }},
		{"firewall-country", func(f *activity.Filter) { f.Country = "YE" }},
		{"server", func(f *activity.Filter) { f.Server = "hs-test" }},
		{"missing-server", func(f *activity.Filter) { f.Server = "-" }},
		{"qtype", func(f *activity.Filter) { f.QType = "A" }},
		{"match", func(f *activity.Filter) { f.Match = "manual-mac" }},
		{"basis", func(f *activity.Filter) { f.IPBasis = "dns-cache" }},
		{"missing-basis", func(f *activity.Filter) { f.IPBasis = "-" }},
		{"combined", func(f *activity.Filter) { f.Kind, f.Domain, f.QType, f.DNSCountry = "dns", "d1.example", "A", "DE" }},
		{"day-filter", func(f *activity.Filter) {
			f.Day = time.UnixMilli(now - 3600000).In(time.FixedZone("", 10800)).Format("2006-01-02")
		}},
		{"hour-filter", func(f *activity.Filter) {
			f.Hour = time.UnixMilli(now - 3600000).In(time.FixedZone("", 10800)).Format("2006-01-02 15:00")
		}},
	}
	for _, group := range []string{"service", "domain", "client", "category", "country", "dnsCountry", "dnsIP", "canonical", "deviceName", "server", "qtype", "match", "ipBasis", "day", "hour"} {
		queries = append(queries, queryCase{"group-" + group, func(f *activity.Filter) { f.View, f.Parts, f.Group = "domains", "table", group }})
	}
	for _, key := range []string{"count", "label", "category", "domains", "devices", "last"} {
		for _, desc := range []bool{false, true} {
			queries = append(queries, queryCase{fmt.Sprintf("sort-%s-%v", key, desc), func(f *activity.Filter) {
				f.View, f.Parts, f.Group, f.Sort, f.Desc, f.Page, f.Limit = "domains", "table", "domain", key, desc, 2, 3
			}})
		}
	}
	queries = append(queries, queryCase{"deep-page", func(f *activity.Filter) { f.View, f.Parts, f.Page = "domains", "table", 100000 }})
	for _, span := range []struct {
		name     string
		from, to int64
	}{{"day", now - 86400000 + 101, now + 201}, {"week", now - 7*86400000 + 101, now + 201}, {"month", now - 30*86400000 + 101, now + 201}, {"custom", now - 8*86400000 + 1234567, now - 2*86400000 + 7654321}, {"partial-second", now - 3600000 + 101, now - 3600000 + 201}} {
		t.Run(span.name, func(t *testing.T) {
			for _, q := range queries {
				t.Run(q.name, func(t *testing.T) {
					f := activity.Filter{From: span.from, To: span.to, View: "summary", Group: "dnsIP", Page: 1, Limit: 200, Desc: true}
					q.edit(&f)
					want, err := activity.Analyze(f, rules, "Asia/Aden", func(from, to int64, fn func(activity.Event) error) error {
						return d.WalkActivity(t.Context(), "r1", from, to, fn)
					}, lookup)
					if err != nil {
						t.Fatal(err)
					}
					got, ready, err := d.ActivityIndexedQuery(t.Context(), "r1", f, rules, "Asia/Aden", "range-geo", lookup)
					if err != nil || !ready {
						t.Fatal(ready, err)
					}
					// Table-only responses include filter choices, not chart rankings.
					if f.Parts == "table" {
						want.Previous = 0
						want.Services, got.Services = nil, nil
						want.Clients, got.Clients = nil, nil
						want.Categories, got.Categories = nil, nil
						want.Countries, got.Countries = nil, nil
						want.DNSCountries, got.DNSCountries = nil, nil
						want.QueryTypes, got.QueryTypes = nil, nil
						want.MatchMethods, got.MatchMethods = nil, nil
						want.IPBases, got.IPBases = nil, nil
						want.Series, got.Series = nil, nil
						want.Heatmap, got.Heatmap = nil, nil
					}
					got.ComputedAt, got.RefreshAt, got.DataRevision, got.TotalStored, got.ResultVersion = 0, 0, 0, 0, ""
					if !reflect.DeepEqual(want, got) {
						a, _ := json.Marshal(want)
						b, _ := json.Marshal(got)
						var wa, ga map[string]any
						json.Unmarshal(a, &wa)
						json.Unmarshal(b, &ga)
						for k, v := range wa {
							if !reflect.DeepEqual(v, ga[k]) {
								t.Errorf("%s differs: want %.500v got %.500v", k, v, ga[k])
							}
						}
					}
				})
			}
			// Event grouping/counts use a different query path from rankings.
			for _, filter := range append([]queryCase{queries[0]}, queries[4:26]...) {
				f := activity.Filter{From: span.from, To: span.to, Page: 1, Limit: 10000, Sort: "last", Desc: true}
				filter.edit(&f)
				f.View, f.Parts = "events", "table"
				want, total, err := d.ActivityEventPage(t.Context(), "r1", f, rules, lookup)
				if err != nil {
					t.Fatal(err)
				}
				got, ready, err := d.ActivityIndexedQuery(t.Context(), "r1", f, rules, "Asia/Aden", "range-geo", lookup)
				if err != nil || !ready {
					t.Fatal(ready, err)
				}
				signatures := func(es []activity.Event) []string {
					out := make([]string, 0, len(es))
					for _, e := range es {
						_, _, fp, err := shapeOf(e)
						if err != nil {
							t.Fatal(err)
						}
						out = append(out, fmt.Sprintf("%d:%s:%d", e.At/1000, fp, e.Count))
					}
					sort.Strings(out)
					return out
				}
				if total != got.TotalRows || !reflect.DeepEqual(signatures(want), signatures(got.Events)) {
					t.Fatal("event grouping or counts differ", filter.name, total, got.TotalRows)
				}
			}
		})
	}
}

func TestActivityCalendarHistogramUsesHourlyPlan(t *testing.T) {
	d, now, rules, lookup := indexedActivityFixture(t)
	f := activity.Filter{From: now - 30*86400000 + 101, To: now + 201, View: "summary", Group: "dnsIP", Parts: "overview", Page: 1, Limit: 10}
	if _, _, err := d.ActivityIndexedQuery(t.Context(), "r1", f, rules, "Asia/Aden", "calendar-plan", lookup); err != nil {
		t.Fatal(err)
	}
	var traces []activityHistogramTrace
	name := fmt.Sprintf("activity_calendar_plan_%d", time.Now().UnixNano())
	sql.Register(name, activityHistogramDriver{&traces})
	h, err := sql.Open(name, d.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.SetMaxOpenConns(1)
	for _, zone := range []string{"Asia/Aden", "Asia/Kathmandu"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		f.Day = time.UnixMilli(now).In(loc).Format("2006-01-02")
		traces = nil
		out := EmptyActivityResult(f)
		if err = activityHistogramQuery(t.Context(), h, "r1", f, zone, &out); err != nil {
			t.Fatal(err)
		}
		if len(traces) != 1 {
			t.Fatal("actual calendar query not observed")
		}
		rows, err := h.Query("EXPLAIN QUERY PLAN "+traces[0].query, traces[0].args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		text := strings.Join(plan, "\n")
		if zone == "Asia/Aden" && (!strings.Contains(traces[0].query, "FROM activity_hour_totals h") || !strings.Contains(text, "SEARCH h USING PRIMARY KEY") || strings.Count(text, "activity_facts_time") != 2) {
			t.Fatal("calendar query stopped using exact hourly totals and boundary seeks", text)
		}
		if zone == "Asia/Kathmandu" && strings.Contains(traces[0].query, "FROM activity_hour_totals h") {
			t.Fatal("fractional offset incorrectly used whole-hour totals")
		}
	}
}

func TestActivityRollupRangesKeepDSTAndFractionalZones(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	now := time.Date(2026, 11, 2, 12, 34, 56, 789000000, time.UTC).UnixMilli()
	var events []activity.Event
	for i := 0; i < 35*96; i++ {
		events = append(events, activity.Event{RouterID: "r1", SourceKey: fmt.Sprint(i), At: now - int64(i)*900000, Received: now, Kind: "dns", Domain: fmt.Sprintf("d%d.example", i%7), Client: "synthetic", DNSIPs: []string{"2001:db8::8"}})
	}
	if err = d.PersistActivity(events, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, zone := range []string{"America/New_York", "Australia/Lord_Howe", "Asia/Kathmandu"} {
		for _, days := range []int64{1, 7, 30} {
			for _, group := range []string{"domain", "day", "hour"} {
				f := activity.Filter{From: now - days*86400000 + 101, To: now + 1, View: "domains", Group: group, Page: 1, Limit: 200, Desc: true}
				want, err := activity.Analyze(f, nil, zone, func(from, to int64, fn func(activity.Event) error) error {
					return d.WalkActivity(t.Context(), "r1", from, to, fn)
				})
				if err != nil {
					t.Fatal(err)
				}
				got, ready, err := d.ActivityIndexedQuery(t.Context(), "r1", f, nil, zone, "", nil)
				if err != nil || !ready {
					t.Fatal(zone, days, group, ready, err)
				}
				if !reflect.DeepEqual(want.Rows, got.Rows) || !reflect.DeepEqual(want.Totals, got.Totals) || !reflect.DeepEqual(want.Series, got.Series) || !reflect.DeepEqual(want.Heatmap, got.Heatmap) || want.Previous != got.Previous {
					t.Fatal("calendar/rollup mismatch", zone, days, group)
				}
			}
		}
	}
}
