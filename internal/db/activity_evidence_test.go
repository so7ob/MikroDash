package db

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"mikrodash/internal/activity"
	"mikrodash/internal/subscriber"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestActivityEvidenceKeyKeepsExactSignedOrder(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	type pair struct{ at, id int64 }
	var values []pair
	for _, at := range []int64{math.MinInt64, -1, 0, 1, 9007199254740993, math.MaxInt64} {
		for _, id := range []int64{math.MinInt64, -1, 0, 1, 9007199254740993, math.MaxInt64} {
			values = append(values, pair{at, id})
		}
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].at < values[j].at || values[i].at == values[j].at && values[i].id < values[j].id
	})
	for i, v := range values {
		key := activityEvidenceKey(v.at, v.id)
		if i > 0 && bytes.Compare(activityEvidenceKey(values[i-1].at, values[i-1].id), key) >= 0 {
			t.Fatal("tuple ordering lost")
		}
		var id int64
		if err = d.sql.QueryRow(`SELECT activity_evidence_id(?)`, key).Scan(&id); err != nil || id != v.id {
			t.Fatal("exact ID lost", v, id, err)
		}
	}
	var id int64
	if err = d.sql.QueryRow(`SELECT activity_evidence_id(X'01')`).Scan(&id); err == nil {
		t.Fatal("incomplete representative was accepted")
	}
}

func legacyActivityEvidenceFixture(t *testing.T) (*DB, int64) {
	t.Helper()
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	now := time.Date(2026, 11, 14, 0, 0, 0, 500000000, time.UTC).UnixMilli()
	var events []activity.Event
	for i, at := range []int64{now - 86400001, now - 3600100, now - 100, now - 100, now - 3600001, now + 1} {
		e := activity.Event{RouterID: "r1", SourceKey: fmt.Sprint(i), At: at, Received: now, Kind: "dns", Domain: "synthetic.example", Client: "same", User: "اسم عربي", IP: "198.51.100.8", DeviceName: "جهاز %_", DNSIPs: []string{"2001:db8::8"}, TimeCertain: true}
		if i == 5 {
			e.Kind = "firewall"
			e.Domain = ""
			e.DNSIPs = nil
			e.Destination = "198.51.100.9:443"
		}
		events = append(events, e)
	}
	if err = d.PersistActivity(events, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	account, _, err := d.Subscribers().CreateAccount(t.Context(), "synthetic-admin", "evidence-migration", "اسم محفوظ", "synthetic-migration-secret", "Synthetic preservation fixture", 0)
	if err != nil {
		t.Fatal(err)
	}
	plan := subscriber.Plan{ID: "evidence-plan", Version: 1, Name: "Synthetic", ValidSeconds: 86400, Start: "assigned", Networks: []string{"synthetic"}, MaxSessions: 1, Down: 1000000, Up: 1000000, Policies: []subscriber.Policy{{ID: "quota", Limits: []subscriber.Limit{{ID: "data", Kind: "total", State: "limited", Amount: 10000, Action: "disconnect", Period: subscriber.Period{Kind: "none", Zone: "UTC", Anchor: time.UnixMilli(now).Add(-time.Hour)}}}}}, Speeds: []subscriber.Speed{{ID: "standard", Name: "Standard", Down: 1000000, Up: 1000000}}}
	if err = d.Subscribers().SavePlan(t.Context(), "synthetic-admin", "Synthetic preservation fixture", plan); err != nil {
		t.Fatal(err)
	}
	subscriptionID, err := d.Subscribers().Assign(t.Context(), "synthetic-admin", account.ID, plan.ID, 1, "Synthetic preservation fixture", time.UnixMilli(now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.sql.Exec(`INSERT INTO subscriber_buckets(subscription_id,limit_id,period_start,used,quality) VALUES(?,'data',?,123,'fresh')`, subscriptionID, time.UnixMilli(now).Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	// Downgrade only new metadata to exercise a genuine nonempty pre-48 layout.
	for _, q := range []string{
		`DROP TRIGGER activity_fact_insert`, `DROP TRIGGER activity_fact_delete`,
		`DROP INDEX activity_facts_time`, `DROP INDEX activity_facts_shape`, `DROP INDEX activity_daily_shape`, `DROP INDEX activity_hourly_shape`, `DROP INDEX activity_shapes_summary`,
		`ALTER TABLE activity_facts DROP COLUMN evidence_key`, `ALTER TABLE activity_daily DROP COLUMN evidence_key`,
		`ALTER TABLE activity_hourly DROP COLUMN groups`, `ALTER TABLE activity_hourly DROP COLUMN last_at`, `ALTER TABLE activity_hourly DROP COLUMN evidence_key`,
		`ALTER TABLE activity_hour_totals DROP COLUMN groups`,
		`CREATE INDEX activity_facts_time ON activity_facts(router_id,at,event_id,shape_id)`,
		`CREATE INDEX activity_facts_shape ON activity_facts(router_id,shape_id,at,event_id)`,
		`CREATE INDEX activity_daily_shape ON activity_daily(router_id,shape_id,day)`,
		`CREATE INDEX activity_hourly_shape ON activity_hourly(router_id,shape_id,hour)`,
		`CREATE TRIGGER activity_fact_insert AFTER INSERT ON activity_facts BEGIN SELECT RAISE(ABORT,'legacy trigger must be replaced'); END`,
		`CREATE TRIGGER activity_fact_delete AFTER DELETE ON activity_facts BEGIN SELECT RAISE(ABORT,'legacy trigger must be replaced'); END`,
		`DELETE FROM schema_version WHERE version>47`,
	} {
		if _, err = d.sql.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	return d, now
}

func activityOriginalSnapshot(t *testing.T, d *DB) []byte {
	t.Helper()
	out := map[string]any{}
	for _, table := range []string{"activity_events", "activity_index_state", "activity_pending", "activity_checkpoints", "subscriber_accounts", "subscriber_plans", "subscriber_subscriptions", "subscriber_buckets", "subscriber_audit", "grants", "role_pages"} {
		rows, err := d.sql.Query("SELECT * FROM " + table + " ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		var data [][]any
		for rows.Next() {
			v := make([]any, len(cols))
			ptr := make([]any, len(v))
			for i := range v {
				ptr[i] = &v[i]
			}
			if err = rows.Scan(ptr...); err != nil {
				t.Fatal(err)
			}
			data = append(data, v)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		out[table] = data
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertActivityEvidenceRollups(t *testing.T, d *DB) {
	t.Helper()
	for _, bucket := range []struct {
		table, col string
		width      int64
	}{{"activity_daily", "day", 86400000}, {"activity_hourly", "hour", 3600000}} {
		rows, err := d.sql.Query(`SELECT router_id,` + bucket.col + `,shape_id,n,groups,last_at,evidence_key FROM ` + bucket.table + ` ORDER BY router_id,` + bucket.col + `,shape_id`)
		if err != nil {
			t.Fatal(err)
		}
		type row struct {
			router              string
			period, shape, last int64
			n, groups           int
			key                 []byte
		}
		var all []row
		for rows.Next() {
			var r row
			if err = rows.Scan(&r.router, &r.period, &r.shape, &r.n, &r.groups, &r.last, &r.key); err != nil {
				t.Fatal(err)
			}
			all = append(all, r)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		for _, r := range all {
			var n, groups int
			var at, id int64
			if err = d.sql.QueryRow(`SELECT COUNT(*),COUNT(DISTINCT at/1000) FROM activity_facts WHERE router_id=? AND shape_id=? AND at>=? AND at<?`, r.router, r.shape, r.period*bucket.width, (r.period+1)*bucket.width).Scan(&n, &groups); err != nil {
				t.Fatal(err)
			}
			if err = d.sql.QueryRow(`SELECT at,event_id FROM activity_facts WHERE router_id=? AND shape_id=? AND at>=? AND at<? ORDER BY at DESC,event_id DESC LIMIT 1`, r.router, r.shape, r.period*bucket.width, (r.period+1)*bucket.width).Scan(&at, &id); err != nil {
				t.Fatal(err)
			}
			if n != r.n || groups != r.groups || r.last != at || !bytes.Equal(r.key, activityEvidenceKey(at, id)) {
				t.Fatalf("%s rollup differs from authoritative facts", bucket.table)
			}
		}
	}
	rows, err := d.sql.Query(`SELECT router_id,hour,kind,n,groups FROM activity_hour_totals`)
	if err != nil {
		t.Fatal(err)
	}
	type total struct {
		router, kind string
		hour         int64
		n, groups    int
	}
	var totals []total
	for rows.Next() {
		var r total
		if err = rows.Scan(&r.router, &r.hour, &r.kind, &r.n, &r.groups); err != nil {
			t.Fatal(err)
		}
		totals = append(totals, r)
	}
	rows.Close()
	for _, r := range totals {
		var n, groups int
		if err = d.sql.QueryRow(`SELECT COUNT(*),COUNT(DISTINCT a.shape_id||':'||(a.at/1000)) FROM activity_facts a JOIN activity_shapes s ON s.id=a.shape_id WHERE a.router_id=? AND a.at>=? AND a.at<? AND s.kind=?`, r.router, r.hour*3600000, (r.hour+1)*3600000, r.kind).Scan(&n, &groups); err != nil {
			t.Fatal(err)
		}
		if n != r.n || groups != r.groups {
			t.Fatal("hour total differs from facts")
		}
	}
}

func TestActivityEvidenceMigrationPreservesDataAndTriggers(t *testing.T) {
	d, now := legacyActivityEvidenceFixture(t)
	before := activityOriginalSnapshot(t, d)
	if n, err := d.Migrate(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if !bytes.Equal(before, activityOriginalSnapshot(t, d)) {
		t.Fatal("source, ledger or grants changed")
	}
	assertActivityEvidenceRollups(t, d)
	for _, q := range []string{`DELETE FROM activity_events WHERE source_key='3'`, `DELETE FROM activity_events WHERE source_key='2'`, `DELETE FROM activity_events WHERE source_key='0'`} {
		if _, err := d.sql.Exec(q); err != nil {
			t.Fatal(err)
		}
		assertActivityEvidenceRollups(t, d)
	}
	e := activity.Event{RouterID: "r1", SourceKey: "new", At: now + 200, Received: now, Kind: "dns", Domain: "synthetic.example", Client: "same", User: "اسم عربي", IP: "198.51.100.8"}
	if err := d.PersistActivity([]activity.Event{e, e}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	assertActivityEvidenceRollups(t, d)
	stable := activityOriginalSnapshot(t, d)
	if _, err := d.sql.Exec(`DELETE FROM schema_version WHERE version>=48`); err != nil {
		t.Fatal(err)
	}
	if n, err := d.Migrate(); err != nil || n != 1 {
		t.Fatal("replay", n, err)
	}
	if !reflect.DeepEqual(stable, activityOriginalSnapshot(t, d)) {
		t.Fatal("replay changed original rows")
	}
	assertActivityEvidenceRollups(t, d)
	if n, err := d.Migrate(); err != nil || n != 0 {
		t.Fatal("not idempotent", n, err)
	}
}

func TestActivityEvidenceMigrationFailureRollsBack(t *testing.T) {
	d, _ := legacyActivityEvidenceFixture(t)
	before := activityOriginalSnapshot(t, d)
	if _, err := d.sql.Exec(`CREATE TEMP TRIGGER fail_evidence_upgrade BEFORE UPDATE ON activity_facts BEGIN SELECT RAISE(ABORT,'synthetic migration fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Migrate(); err == nil {
		t.Fatal("fault hidden")
	}
	if v, err := d.SchemaVersion(); err != nil || v != 47 {
		t.Fatal("partial migration stamped", v, err)
	}
	if _, err := d.sql.Exec(`SELECT evidence_key FROM activity_facts`); err == nil {
		t.Fatal("schema alteration leaked through rollback")
	}
	if !bytes.Equal(before, activityOriginalSnapshot(t, d)) {
		t.Fatal("failed migration changed source")
	}
	if _, err := d.sql.Exec(`DROP TRIGGER fail_evidence_upgrade`); err != nil {
		t.Fatal(err)
	}
	if n, err := d.Migrate(); err != nil || n != 1 {
		t.Fatal("retry failed", n, err)
	}
	assertActivityEvidenceRollups(t, d)
}
