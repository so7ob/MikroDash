package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"mikrodash/internal/activity"
)

// One additive schema for both new and upgrading installations.
const activityDDL = `
CREATE TABLE IF NOT EXISTS activity_settings (
 router_id TEXT PRIMARY KEY, enabled INTEGER NOT NULL DEFAULT 0,
 timezone TEXT NOT NULL DEFAULT '', folder TEXT NOT NULL DEFAULT 'usb1-part1/Log', invalid INTEGER NOT NULL DEFAULT 0,
 dropped INTEGER NOT NULL DEFAULT 0, last_received INTEGER NOT NULL DEFAULT 0,
 last_persisted INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS activity_checkpoints (
 router_id TEXT NOT NULL, generation TEXT NOT NULL, payload TEXT NOT NULL,
 PRIMARY KEY(router_id,generation)
);
CREATE TABLE IF NOT EXISTS activity_events (
 id INTEGER PRIMARY KEY, router_id TEXT NOT NULL, source_key TEXT,
 at INTEGER NOT NULL, received INTEGER NOT NULL, kind TEXT NOT NULL,
 payload TEXT NOT NULL, UNIQUE(router_id,source_key)
);
CREATE INDEX IF NOT EXISTS activity_events_time ON activity_events(router_id,at,id);
CREATE INDEX IF NOT EXISTS activity_events_received ON activity_events(received);
CREATE TABLE IF NOT EXISTS activity_rules (
 router_id TEXT NOT NULL, domain TEXT NOT NULL, subdomains INTEGER NOT NULL,
 service TEXT NOT NULL, category TEXT NOT NULL, PRIMARY KEY(router_id,domain)
);
CREATE TABLE IF NOT EXISTS activity_coverage (
 router_id TEXT NOT NULL, started_at INTEGER NOT NULL, ended_at INTEGER NOT NULL,
 PRIMARY KEY(router_id,started_at)
);
`

type ActivityState struct {
	activity.Settings
	Invalid       int64 `json:"invalid"`
	Dropped       int64 `json:"dropped"`
	LastReceived  int64 `json:"lastReceived"`
	LastPersisted int64 `json:"lastPersisted"`
}

func (d *DB) ActivityState(router string) (v ActivityState, err error) {
	err = d.sql.QueryRow(`SELECT enabled,timezone,folder,invalid,dropped,last_received,last_persisted FROM activity_settings WHERE router_id=?`, router).Scan(&v.Enabled, &v.Timezone, &v.Folder, &v.Invalid, &v.Dropped, &v.LastReceived, &v.LastPersisted)
	if err == sql.ErrNoRows {
		err = nil
	}
	if v.Folder == "" {
		v.Folder = activity.DefaultFolder
	}
	return
}
func (d *DB) ActivityEnabled() (map[string]activity.Settings, error) {
	rows, err := d.sql.Query(`SELECT router_id,enabled,timezone,folder FROM activity_settings WHERE enabled=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]activity.Settings{}
	for rows.Next() {
		var id string
		var v activity.Settings
		if err = rows.Scan(&id, &v.Enabled, &v.Timezone, &v.Folder); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}
func (d *DB) SetActivitySettings(router string, v activity.Settings) error {
	_, err := d.sql.Exec(`INSERT INTO activity_settings(router_id,enabled,timezone,folder) VALUES(?,?,?,?) ON CONFLICT(router_id) DO UPDATE SET enabled=excluded.enabled,timezone=excluded.timezone,folder=excluded.folder`, router, v.Enabled, v.Timezone, v.Folder)
	return err
}

// PersistActivity is atomic: retries can replay identified rows safely and
// counters/coverage cannot claim an event was stored before its transaction.
func (d *DB) PersistActivity(events []activity.Event, statuses map[string]activity.Status, coverage map[string]activity.Coverage, checkpoints map[string]activity.Checkpoint) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO activity_events(router_id,source_key,at,received,kind,payload) VALUES(?,?,?,?,?,?) ON CONFLICT(router_id,source_key) DO NOTHING`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	enrich, err := tx.Prepare(`UPDATE activity_events SET payload=json_patch(payload,?) WHERE router_id=? AND source_key=? AND kind='dns'`)
	if err != nil {
		return err
	}
	defer enrich.Close()
	for _, e := range events {
		if e.Replay {
			// Patch only newly retained metadata. Original time, reception, identity
			// and counts remain untouched; absent/pruned source rows stay absent.
			patch := map[string]any{}
			if e.Canonical != "" {
				patch["canonical"] = e.Canonical
			}
			if e.DeviceName != "" {
				patch["deviceName"] = e.DeviceName
			}
			if e.IPBasis != "" {
				patch["ipBasis"] = e.IPBasis
			}
			if len(e.DNSIPs) > 0 {
				patch["dnsIPs"] = e.DNSIPs
			}
			if len(patch) > 0 && e.SourceKey != "" {
				body, err := json.Marshal(patch)
				if err != nil {
					return err
				}
				if _, err = enrich.Exec(string(body), e.RouterID, e.SourceKey); err != nil {
					return err
				}
			}
			continue
		}
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		var key any
		if e.SourceKey != "" {
			key = e.SourceKey
		}
		if _, err = stmt.Exec(e.RouterID, key, e.At, e.Received, e.Kind, string(b)); err != nil {
			return err
		}
	}
	for id, s := range statuses {
		if _, err = tx.Exec(`UPDATE activity_settings SET invalid=?,dropped=?,last_received=?,last_persisted=? WHERE router_id=?`, s.Invalid, s.Dropped, s.LastReceived, s.LastPersisted, id); err != nil {
			return err
		}
	}
	for id, c := range coverage {
		if c.Start > 0 {
			if _, err = tx.Exec(`INSERT INTO activity_coverage(router_id,started_at,ended_at) VALUES(?,?,?) ON CONFLICT(router_id,started_at) DO UPDATE SET ended_at=excluded.ended_at`, id, c.Start, c.End); err != nil {
				return err
			}
		}
	}
	for router, c := range checkpoints {
		b, _ := json.Marshal(c)
		if _, err = tx.Exec(`INSERT INTO activity_checkpoints(router_id,generation,payload) VALUES(?,?,?) ON CONFLICT(router_id,generation) DO UPDATE SET payload=excluded.payload`, router, c.Generation, string(b)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// WalkActivity releases the single SQLite connection between bounded keyset
// batches. Consumers can recheck permissions and the archive writer can commit
// while a long export/query is being streamed.
func (d *DB) WalkActivity(ctx context.Context, router string, from, to int64, fn func(activity.Event) error) error {
	cursorAt := to
	cursorID := int64(9223372036854775807)
	for {
		rows, err := d.sql.QueryContext(ctx, `SELECT id,at,payload FROM activity_events WHERE router_id=? AND at>=? AND at<? AND (at<? OR (at=? AND id<?)) ORDER BY at DESC,id DESC LIMIT 1000`, router, from, to, cursorAt, cursorAt, cursorID)
		if err != nil {
			return err
		}
		batch := make([]activity.Event, 0, 1000)
		for rows.Next() {
			var e activity.Event
			var b string
			var at int64
			var id int64
			if err = rows.Scan(&id, &at, &b); err != nil {
				rows.Close()
				return err
			}
			if err = json.Unmarshal([]byte(b), &e); err != nil {
				rows.Close()
				return err
			}
			e.ID = id
			batch = append(batch, e)
			cursorAt = at
			cursorID = id
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, e := range batch {
			if err = fn(e); err != nil {
				return err
			}
		}
		if len(batch) < 1000 {
			return nil
		}
	}
}

func (d *DB) ActivityCoverage(router string, from, to int64) ([]activity.Coverage, error) {
	rows, err := d.sql.Query(`SELECT started_at,ended_at FROM activity_coverage WHERE router_id=? AND ended_at>=? AND started_at<? ORDER BY started_at`, router, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []activity.Coverage{}
	for rows.Next() {
		var c activity.Coverage
		if err = rows.Scan(&c.Start, &c.End); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (d *DB) ActivityRules(router string) ([]activity.Rule, error) {
	rows, err := d.sql.Query(`SELECT domain,subdomains,service,category FROM activity_rules WHERE router_id=? ORDER BY domain`, router)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []activity.Rule{}
	for rows.Next() {
		var r activity.Rule
		r.Custom = true
		if err = rows.Scan(&r.Domain, &r.Subdomains, &r.Service, &r.Category); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (d *DB) SetActivityRule(router string, r activity.Rule) error {
	_, err := d.sql.Exec(`INSERT INTO activity_rules(router_id,domain,subdomains,service,category) VALUES(?,?,?,?,?) ON CONFLICT(router_id,domain) DO UPDATE SET subdomains=excluded.subdomains,service=excluded.service,category=excluded.category`, router, r.Domain, r.Subdomains, r.Service, r.Category)
	return err
}
func (d *DB) DeleteActivityRule(router, domain string) error {
	_, err := d.sql.Exec(`DELETE FROM activity_rules WHERE router_id=? AND domain=?`, router, domain)
	return err
}

// This sweep touches only activity's own event/counter/coverage data. Event
// retention uses reception so an uncertain router clock cannot retain forever.
func (d *DB) PruneActivity(now int64) error {
	cutoff := now - int64(activity.Retention/time.Millisecond)
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM activity_events WHERE received<?`, `DELETE FROM activity_coverage WHERE ended_at<?`} {
		if _, err = tx.Exec(q, cutoff); err != nil {
			return err
		}
	}
	return tx.Commit()
}
