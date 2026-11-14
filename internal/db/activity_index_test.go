package db

import (
	"context"
	"encoding/json"
	"fmt"
	"mikrodash/internal/activity"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func indexedActivityFixture(t *testing.T) (*DB, int64, []activity.Rule, func(string) (string, string)) {
	t.Helper()
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	now := time.Now().Truncate(time.Hour).UnixMilli()
	if err = d.SetActivitySettings("r1", activity.Settings{Timezone: "Asia/Aden"}); err != nil {
		t.Fatal(err)
	}
	events := []activity.Event{}
	for i := 0; i < 320; i++ {
		e := activity.Event{RouterID: "r1", SourceKey: fmt.Sprint(i), At: now - int64(i%70)*3600000 + int64(i%3)*100, Received: now, Kind: "dns", Domain: fmt.Sprintf("d%d.example", i%7), User: fmt.Sprintf("مستخدم %d", i%3), Server: "hs-test", IP: fmt.Sprintf("198.51.100.%d", i%4+1), MAC: fmt.Sprintf("02:00:00:00:00:%02d", i%4+1), QType: "A", Match: "manual-mac", DNSIPs: []string{"198.51.100.8", "2001:db8::8"}, Canonical: "أمثلة.example", DeviceName: "جهاز %_ Aa", IPBasis: "dns-cache", TimeCertain: true}
		if i%5 == 0 {
			e.User = ""
		}
		if i%11 == 0 {
			e.DNSIPs = nil
			e.Canonical = ""
			e.DeviceName = ""
			e.IPBasis = ""
		}
		if i%13 == 0 {
			e.Kind = "firewall"
			e.Domain = ""
			e.DNSIPs = nil
			e.Destination = "198.51.100.9:443"
			e.Protocol = "tcp"
			e.QType = ""
			e.Match = ""
			e.Canonical = ""
			e.DeviceName = ""
			e.IPBasis = ""
		}
		e.Client = activity.Identity(e)
		events = append(events, e)
		if i%9 == 0 {
			e.SourceKey += "duplicate"
			events = append(events, e)
		}
	}
	if err = d.PersistActivity(events, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	rules := []activity.Rule{{Domain: "d1.example", Subdomains: true, Service: "Service خاص", Category: "Research", Custom: true}}
	lookup := func(ip string) (string, string) {
		if ip == "2001:db8::8" {
			return "DE", "Berlin"
		}
		if ip == "198.51.100.9" {
			return "YE", "Aden"
		}
		return "", ""
	}
	return d, now, rules, lookup
}
func TestIndexedActivityMatchesStreamingAnalysis(t *testing.T) {
	d, now, rules, lookup := indexedActivityFixture(t)
	ctx := context.Background()
	base := activity.Filter{From: now - 24*3600000, To: now + 1000, View: "summary", Group: "dnsIP", Page: 1, Limit: 200, Desc: true}
	filters := []activity.Filter{base}
	for _, change := range []func(*activity.Filter){
		func(f *activity.Filter) { f.Search = "جهاز %_ aa" }, func(f *activity.Filter) { f.Search = "خاص" }, func(f *activity.Filter) { f.DNSCountry = "DE" }, func(f *activity.Filter) { f.Country = "YE" }, func(f *activity.Filter) { f.DNSIP = "2001:db8::8" }, func(f *activity.Filter) { f.Kind = "firewall" }, func(f *activity.Filter) { f.Domain = "d1.example" }, func(f *activity.Filter) { f.Service = "Service خاص" }, func(f *activity.Filter) { f.Category = "Research" }, func(f *activity.Filter) { f.Canonical = "أمثلة.example" }, func(f *activity.Filter) { f.IPBasis = "-" }, func(f *activity.Filter) { f.QType = "A" }, func(f *activity.Filter) { f.Match = "manual-mac" }, func(f *activity.Filter) { f.Server = "hs-test" }, func(f *activity.Filter) {
			f.Day = time.UnixMilli(now).In(time.FixedZone("", 10800)).Format("2006-01-02")
		}, func(f *activity.Filter) {
			f.Hour = time.UnixMilli(now).In(time.FixedZone("", 10800)).Format("2006-01-02 15:00")
		},
	} {
		f := base
		change(&f)
		filters = append(filters, f)
	}
	for _, group := range []string{"service", "domain", "client", "category", "country", "dnsCountry", "day", "hour", "canonical", "deviceName", "server", "qtype", "match", "ipBasis"} {
		f := base
		f.View = "domains"
		f.Group = group
		filters = append(filters, f)
	}
	for i, f := range filters {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			want, err := activity.Analyze(f, rules, "Asia/Aden", func(from, to int64, fn func(activity.Event) error) error {
				return d.WalkActivity(ctx, "r1", from, to, fn)
			}, lookup)
			if err != nil {
				t.Fatal(err)
			}
			got, ready, err := d.ActivityIndexedQuery(ctx, "r1", f, rules, "Asia/Aden", "geo-one", lookup)
			if err != nil || !ready {
				t.Fatal(ready, err)
			}
			got.ComputedAt = 0
			got.RefreshAt = 0
			got.DataRevision = 0
			got.TotalStored = 0
			got.ResultVersion = ""
			if !reflect.DeepEqual(want, got) {
				a, _ := json.Marshal(want)
				b, _ := json.Marshal(got)
				var wa, ga map[string]any
				json.Unmarshal(a, &wa)
				json.Unmarshal(b, &ga)
				for k, v := range wa {
					if !reflect.DeepEqual(v, ga[k]) {
						t.Errorf("%s differs: want %.1000v got %.1000v", k, v, ga[k])
					}
				}
			}
		})
	}
}
func TestIndexedEventsPreserveCountsAndBoundaries(t *testing.T) {
	d, now, rules, lookup := indexedActivityFixture(t)
	ctx := context.Background()
	for _, shift := range []int64{0, 1, 101, 201} {
		f := activity.Filter{From: now - 24*3600000 + shift, To: now + 201, View: "events", Page: 1, Limit: 1000, Sort: "last", Desc: true, Parts: "table"}
		want, total, err := d.ActivityEventPage(ctx, "r1", f, rules, lookup)
		if err != nil {
			t.Fatal(err)
		}
		got, ready, err := d.ActivityIndexedQuery(ctx, "r1", f, rules, "Asia/Aden", "geo-one", lookup)
		if err != nil || !ready {
			t.Fatal(ready, err)
		}
		if total != got.TotalRows || len(want) != len(got.Events) {
			t.Fatalf("boundary %d counts %d/%d vs %d/%d", shift, total, len(want), got.TotalRows, len(got.Events))
		}
		signature := func(events []activity.Event) []string {
			out := []string{}
			for _, e := range events {
				_, _, fp, err := shapeOf(e)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, fmt.Sprintf("%d:%s:%d", e.At/1000, fp, e.Count))
			}
			sort.Strings(out)
			return out
		}
		if !reflect.DeepEqual(signature(want), signature(got.Events)) {
			t.Fatal("event grouping changed")
		}
	}
}
func TestIndexedEventsCursorAndContextIsolation(t *testing.T) {
	d, now, rules, lookup := indexedActivityFixture(t)
	ctx := context.Background()
	for _, sortKey := range []string{"last", "count", "kind", "label", "user"} {
		for _, desc := range []bool{true, false} {
			f := activity.Filter{From: now - 72*3600000, To: now + 1000, View: "events", Parts: "table", Page: 1, Limit: 11, Sort: sortKey, Desc: desc}
			first, _, err := d.ActivityIndexedQuery(ctx, "r1", f, rules, "Asia/Aden", "geo-one", lookup)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[int64]bool{}
			page := first
			for i := 0; i < 100; i++ {
				for _, e := range page.Events {
					if seen[e.ID] {
						t.Fatalf("%s/%v repeated %d", sortKey, desc, e.ID)
					}
					seen[e.ID] = true
				}
				if len(page.Events) < f.Limit {
					break
				}
				f.Cursor = page.NextCursor
				page, _, err = d.ActivityIndexedQuery(ctx, "r1", f, rules, "Asia/Aden", "geo-one", lookup)
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(seen) != first.TotalRows {
				t.Fatalf("%s/%v visited %d of %d", sortKey, desc, len(seen), first.TotalRows)
			}
			f.Cursor = first.NextCursor
			second, _, err := d.ActivityIndexedQuery(ctx, "r1", f, rules, "Asia/Aden", "geo-one", lookup)
			if err != nil {
				t.Fatal(err)
			}
			f.Cursor = second.PrevCursor
			back, _, err := d.ActivityIndexedQuery(ctx, "r1", f, rules, "Asia/Aden", "geo-one", lookup)
			if err != nil || !reflect.DeepEqual(first.Events, back.Events) {
				t.Fatalf("%s/%v previous cursor %v", sortKey, desc, err)
			}
			f.Domain = "changed.example"
			if _, _, err = d.ActivityIndexedQuery(ctx, "r1", f, rules, "Asia/Aden", "geo-one", lookup); err != ErrActivityCursor {
				t.Fatalf("cursor accepted changed filter: %v", err)
			}
		}
	}
}
func TestActivityBackfillResumesAndNeverReturnsPartialHistory(t *testing.T) {
	d, now, rules, lookup := indexedActivityFixture(t)
	ctx := context.Background()
	if _, err := d.sql.Exec(`DROP TRIGGER activity_source_insert; DROP TRIGGER activity_source_update; DROP TRIGGER activity_source_delete; DROP TRIGGER activity_fact_insert; DROP TRIGGER activity_fact_delete;
 DROP TABLE activity_projection_context; DROP TABLE activity_hour_totals; DROP TABLE activity_daily; DROP TABLE activity_hourly; DROP TABLE activity_pending; DROP TABLE activity_facts; DROP TABLE activity_event_groups; DROP TABLE activity_shape_answers; DROP TABLE activity_shapes; DROP TABLE activity_index_state; DELETE FROM schema_version WHERE version>44;`); err != nil {
		t.Fatal(err)
	}
	if n, err := d.Migrate(); err != nil || n != schemaVersion-44 {
		t.Fatal("migration", n, err)
	}
	f := activity.Filter{From: now - 72*3600000, To: now + 1000, View: "events", Page: 1, Limit: 50, Desc: true, Parts: "table"}
	_, ready, err := d.ActivityIndexedQuery(ctx, "r1", f, rules, "Asia/Aden", "g", lookup)
	if err != nil || ready {
		t.Fatal("partial projection served", ready, err)
	}
	n, err := d.IndexActivityBatch(ctx, 7)
	if err != nil || n != 7 {
		t.Fatal(n, err)
	}
	path := d.Path()
	d.Close()
	reopened, err := Open(path[:len(path)-len("/mikrodash.db")])
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for {
		n, err = reopened.IndexActivityBatch(ctx, 29)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	out, ready, err := reopened.ActivityIndexedQuery(ctx, "r1", f, rules, "Asia/Aden", "g", lookup)
	if err != nil || !ready || out.TotalRows == 0 {
		t.Fatal(ready, out.TotalRows, err)
	}
	state, err := reopened.ActivityIndexState(ctx, "r1")
	if err != nil || state.Pending != 0 || state.Total != int64(out.Totals.DNS+out.Totals.Firewall) {
		t.Fatal(state, out.Totals, err)
	}
}

func TestActivityCacheRevisionsRulesGeoAndCancellation(t *testing.T) {
	d, now, rules, lookup := indexedActivityFixture(t)
	f := activity.Filter{From: now - 72*3600000, To: now + 1000, View: "summary", Group: "domain", Page: 1, Limit: 200, Desc: true}
	query := func() activity.Result {
		t.Helper()
		r, ready, err := d.ActivityIndexedQuery(t.Context(), "r1", f, rules, "Asia/Aden", "geo", lookup)
		if err != nil || !ready {
			t.Fatal(ready, err)
		}
		return r
	}
	first := query()
	second := query()
	if first.ComputedAt != second.ComputedAt || len(d.activityCache.entries) == 0 {
		t.Fatal("cache never reused")
	}
	second.Rows[0].Count = -99
	if query().Rows[0].Count < 0 {
		t.Fatal("shared result mutated through a caller")
	}
	if err := d.PersistActivity(nil, map[string]activity.Status{"r1": {LastReceived: now}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if query().DataRevision != first.DataRevision {
		t.Fatal("status-only persistence invalidated data")
	}
	e := activity.Event{RouterID: "r1", SourceKey: "live", At: now + 1, Received: now, Kind: "dns", Domain: "d1.example", User: "live", Client: "live"}
	if err := d.PersistActivity([]activity.Event{e}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	next := query()
	if next.Totals.DNS != first.Totals.DNS+1 || next.DataRevision <= first.DataRevision {
		t.Fatal("new import missed invalidation")
	}
	if err := d.PersistActivity([]activity.Event{e}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if query().DataRevision != next.DataRevision {
		t.Fatal("replayed identity recounted")
	}
	rules = []activity.Rule{{Domain: "d1.example", Service: "Changed", Category: "Changed", Custom: true}}
	f.Category = "Changed"
	changed := query()
	if changed.Totals.DNS == 0 || len(changed.Categories) != 1 || changed.Categories[0].Key != "Changed" {
		t.Fatal("history did not reclassify")
	}
	f.Category = ""
	f.DNSCountry = "DE"
	r, ready, err := d.ActivityIndexedQuery(t.Context(), "r1", f, rules, "Asia/Aden", "different-geo", func(string) (string, string) { return "YE", "Aden" })
	if err != nil || !ready || r.Totals.DNS != 0 {
		t.Fatal("GeoIP key reused stale answers", ready, err)
	}
	d.activityMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = d.ActivityIndexedQuery(ctx, "r1", f, rules, "UTC", "geo", lookup)
	d.activityMu.Unlock()
	if err != context.Canceled {
		t.Fatal("queued canceled query did not return", err)
	}
}

func TestActivityAnchorSurvivesImportAndRetention(t *testing.T) {
	d, now, rules, lookup := indexedActivityFixture(t)
	f := activity.Filter{From: now - 72*3600000, To: now + 1000, View: "events", Parts: "table", Sort: "last", Desc: true, Page: 1, Limit: 11}
	query := func() activity.Result {
		t.Helper()
		r, ready, err := d.ActivityIndexedQuery(t.Context(), "r1", f, rules, "Asia/Aden", "geo", lookup)
		if err != nil || !ready {
			t.Fatal(ready, err)
		}
		return r
	}
	first := query()
	f.Cursor = first.NextCursor
	f.Page = 2
	second := query()
	e := activity.Event{RouterID: "r1", SourceKey: "newest", At: now + 500, Received: now, Kind: "dns", Domain: "new.example"}
	if err := d.PersistActivity([]activity.Event{e}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	f.Cursor = ""
	f.Anchor = second.Anchor
	if got := query(); got.Events[0].ID != second.Events[0].ID {
		t.Fatal("live insert displaced reference")
	}
	anchor, err := decodeActivityCursor(second.Anchor, cursorKey("r1", f))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.sql.Exec(`DELETE FROM activity_events WHERE router_id='r1' AND at<?`, anchor.Second*1000+1000); err != nil {
		t.Fatal(err)
	}
	got := query()
	if len(got.Events) == 0 || got.Events[0].At <= anchor.Second*1000 || got.HasNext {
		t.Fatal("expired reference did not seek nearest retained rows")
	}
	var orphan int
	if err = d.sql.QueryRow(`SELECT COUNT(*) FROM activity_event_groups g WHERE NOT EXISTS(SELECT 1 FROM activity_facts a WHERE a.router_id=g.router_id AND a.shape_id=g.shape_id AND a.at/1000=g.second)`).Scan(&orphan); err != nil || orphan != 0 {
		t.Fatal("retention left stale groups", orphan, err)
	}
}

func TestActivityTimezoneBucketsAndCacheBounds(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	events := []activity.Event{}
	for i := 0; i < 120; i++ {
		events = append(events, activity.Event{RouterID: "r1", SourceKey: fmt.Sprint(i), At: start + int64(i)*900000, Received: start, Kind: "dns", Domain: "example.test"})
	}
	if err = d.PersistActivity(events, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, zone := range []string{"America/New_York", "Australia/Lord_Howe", "Asia/Kathmandu"} {
		f := activity.Filter{From: start, To: start + 30*3600000, View: "domains", Group: "hour", Page: 1, Limit: 200}
		want, err := activity.Analyze(f, nil, zone, func(from, to int64, fn func(activity.Event) error) error {
			return d.WalkActivity(t.Context(), "r1", from, to, fn)
		})
		if err != nil {
			t.Fatal(err)
		}
		got, ready, err := d.ActivityIndexedQuery(t.Context(), "r1", f, nil, zone, "", nil)
		if err != nil || !ready {
			t.Fatal(ready, err)
		}
		if !reflect.DeepEqual(want.Series, got.Series) || !reflect.DeepEqual(want.Heatmap, got.Heatmap) || !reflect.DeepEqual(want.Rows, got.Rows) {
			t.Fatal("timezone boundaries changed", zone)
		}
	}
	cache := activityQueryCache{}
	for i := 0; i < 300; i++ {
		cache.put(fmt.Sprint(i), EmptyActivityResult(activity.Filter{}), 1000)
	}
	if len(cache.entries) != 128 || cache.bytes > activityCacheBudget {
		t.Fatal("unbounded cache")
	}
	if _, ok := cache.get("299", 1000); ok {
		t.Fatal("expired result reused")
	}
}

func TestActivityProjectionFailureRollsBackSourceAndCheckpoint(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err = d.sql.Exec(`CREATE TRIGGER fail_projection BEFORE INSERT ON activity_facts BEGIN SELECT RAISE(ABORT,'synthetic projection failure'); END;`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	e := activity.Event{RouterID: "r1", SourceKey: "atomic", At: now, Received: now, Kind: "dns", Domain: "atomic.example"}
	if err = d.PersistActivity([]activity.Event{e}, map[string]activity.Status{"r1": {Invalid: 5}}, nil, map[string]activity.Checkpoint{"r1": {Generation: "atomic"}}); err == nil {
		t.Fatal("failure fixture did not reject projection")
	}
	var count int
	if err = d.sql.QueryRow(`SELECT COUNT(*) FROM activity_events`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial source commit", count, err)
	}
	checkpoints, err := d.ActivityCheckpoints("r1")
	if err != nil || len(checkpoints) != 0 {
		t.Fatal("partial checkpoint commit", checkpoints, err)
	}
	state, err := d.ActivityIndexState(t.Context(), "r1")
	if err != nil || state.Total != 0 || state.Pending != 0 {
		t.Fatal("partial revision/projection commit", state, err)
	}
	if _, err = d.sql.Exec(`DROP TRIGGER fail_projection`); err != nil {
		t.Fatal(err)
	}
	if err = d.PersistActivity([]activity.Event{e}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	state, err = d.ActivityIndexState(t.Context(), "r1")
	if err != nil || state.Total != 1 || state.Pending != 0 {
		t.Fatal("retry failed", state, err)
	}
}

func TestActivityDerivedContextSurvivesRestartAndRollingExpiry(t *testing.T) {
	d, now, rules, lookup := indexedActivityFixture(t)
	f := activity.Filter{From: now - 72*3600000, To: now + 1000, View: "summary", Group: "domain", Page: 1, Limit: 20}
	if _, ready, err := d.ActivityIndexedQuery(t.Context(), "r1", f, rules, "Asia/Aden", "stable-geo", lookup); err != nil || !ready {
		t.Fatal(ready, err)
	}
	if _, err := d.sql.Exec(`CREATE TRIGGER reject_rebuild BEFORE UPDATE OF ready ON activity_shapes WHEN new.ready=0 BEGIN SELECT RAISE(ABORT,'unexpected rebuild'); END;`); err != nil {
		t.Fatal(err)
	}
	path := d.Path()
	d.Close()
	reopened, err := Open(path[:len(path)-len("/mikrodash.db")])
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, ready, err := reopened.ActivityIndexedQuery(t.Context(), "r1", f, rules, "Asia/Aden", "stable-geo", lookup); err != nil || !ready {
		t.Fatal("unchanged persisted context rebuilt", ready, err)
	}
	future := activity.Event{RouterID: "future", At: now + 1000, Received: now, Kind: "dns", Domain: "future.example"}
	if err = reopened.PersistActivity([]activity.Event{future}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	rolling := activity.Filter{From: now - 86400000 + 1, To: now + 1, RollingDays: 1}
	expires, err := reopened.activityExpiry(t.Context(), "future", rolling, now)
	if err != nil || expires != now+1000 {
		t.Fatal("future event entry reused stale window", expires-now, err)
	}
}

// Slow local enrichment must commit progress without returning partial analytics.
func TestActivityPreparationResumesWithoutPartialResults(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	now := time.Now().UnixMilli()
	events := make([]activity.Event, 600)
	for i := range events {
		events[i] = activity.Event{RouterID: "r1", SourceKey: fmt.Sprint(i), At: now - 100, Received: now, Kind: "dns", Domain: "synthetic.example", Client: "synthetic", DNSIPs: []string{fmt.Sprintf("2001:db8::%x", i+1)}}
	}
	if err = d.PersistActivity(events, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	f := activity.Filter{From: now - 1000, To: now + 1, View: "summary", Group: "dnsIP", Limit: 10, Page: 1, Parts: "overview"}
	before, err := d.ActivityIndexState(t.Context(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	slow := func(string) (string, string) { time.Sleep(5 * time.Millisecond); return "YE", "Synthetic" }
	out, ready, err := d.ActivityIndexedQuery(t.Context(), "r1", f, nil, "UTC", "slow-geo", slow)
	if err != nil || !ready || !out.Preparing || out.PreparationPending <= 0 || out.PreparationPending >= int64(len(events)) {
		t.Fatalf("no resumable progress: ready=%v pending=%d preparing=%v err=%v", ready, out.PreparationPending, out.Preparing, err)
	}
	if out.Totals.DNS != 0 || len(out.Rows) != 0 || len(out.Series) != 0 || len(out.Services) != 0 || out.ResultVersion != "" || out.RefreshAt <= out.ComputedAt {
		t.Fatalf("incomplete projection represented as results: %#v", out)
	}
	pending := out.PreparationPending
	complete := false
	for i := 0; i < 6; i++ {
		out, ready, err = d.ActivityIndexedQuery(t.Context(), "r1", f, nil, "UTC", "slow-geo", slow)
		if err != nil || !ready {
			t.Fatal(ready, err)
		}
		if !out.Preparing {
			complete = true
			break
		}
		if out.PreparationPending >= pending {
			t.Fatalf("retry did not advance: %d >= %d", out.PreparationPending, pending)
		}
		pending = out.PreparationPending
	}
	if !complete || out.Totals.DNS != len(events) || out.TotalRows != len(events) {
		t.Fatalf("complete retry incorrect: preparing=%v dns=%d rows=%d", out.Preparing, out.Totals.DNS, out.TotalRows)
	}
	after, err := d.ActivityIndexState(t.Context(), "r1")
	if err != nil || before != after {
		t.Fatalf("preparation changed source: before=%+v after=%+v err=%v", before, after, err)
	}
	cached, _, err := d.ActivityIndexedQuery(t.Context(), "r1", f, nil, "UTC", "slow-geo", slow)
	if err != nil || cached.Preparing || cached.ResultVersion != out.ResultVersion {
		t.Fatal("pending reply cached", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err = d.ActivityIndexedQuery(ctx, "r1", f, nil, "UTC", "different", slow); err != context.Canceled {
		t.Fatalf("caller cancellation masked: %v", err)
	}
}

func TestActivityContextInvalidationResumesAfterFaultAndRestart(t *testing.T) {
	d, _, rules, lookup := indexedActivityFixture(t)
	if err := d.PrepareActivityShapes(t.Context(), "r1", "old-context", "UTC", rules, lookup); err != nil {
		t.Fatal(err)
	}
	before, err := d.ActivityIndexState(t.Context(), "r1")
	if err != nil {
		t.Fatal(err)
	}
	var cutoff int64
	if err = d.sql.QueryRow(`SELECT id FROM activity_shapes WHERE router_id='r1' ORDER BY id LIMIT 1 OFFSET 99`).Scan(&cutoff); err != nil {
		t.Fatal(err)
	}
	trigger := fmt.Sprintf(`CREATE TEMP TRIGGER injected_activity_reset_failure BEFORE UPDATE OF ready ON activity_shapes WHEN OLD.ready=1 AND NEW.ready=0 AND OLD.id>%d BEGIN SELECT RAISE(ABORT,'synthetic context reset failure'); END`, cutoff)
	if _, err = d.sql.Exec(trigger); err != nil {
		t.Fatal(err)
	}
	if err = d.PrepareActivityShapes(t.Context(), "r1", "new-context", "UTC", rules, lookup); err == nil {
		t.Fatal("permanent storage fault hidden")
	}
	var pending int
	if err = d.sql.QueryRow(`SELECT COUNT(*) FROM activity_shapes WHERE router_id='r1' AND ready=0`).Scan(&pending); err != nil || pending != 100 {
		t.Fatalf("first committed invalidation lost: %d %v", pending, err)
	}
	var marker string
	if err = d.sql.QueryRow(`SELECT version FROM activity_projection_context WHERE router_id='r1'`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	var reset activityShapeReset
	if json.Unmarshal([]byte(marker), &reset) != nil || reset.Version != "new-context" || reset.After != cutoff {
		t.Fatalf("missing durable context reset: %q", marker)
	}
	dir := filepath.Dir(d.Path())
	d.Close()
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.PrepareActivityShapes(t.Context(), "r1", "new-context", "UTC", rules, lookup); err != nil {
		t.Fatal(err)
	}
	if err = reopened.sql.QueryRow(`SELECT COUNT(*) FROM activity_shapes WHERE router_id='r1' AND ready=0`).Scan(&pending); err != nil || pending != 0 {
		t.Fatal("restart incomplete", pending, err)
	}
	if err = reopened.sql.QueryRow(`SELECT version FROM activity_projection_context WHERE router_id='r1'`).Scan(&marker); err != nil || marker != "new-context" {
		t.Fatal("context not finalized", marker, err)
	}
	after, err := reopened.ActivityIndexState(t.Context(), "r1")
	if err != nil || before != after {
		t.Fatal("source changed during reset", before, after, err)
	}
}
