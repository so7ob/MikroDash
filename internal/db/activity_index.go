package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"mikrodash/internal/activity"
	"strings"
)

type ActivityIndexState struct {
	Revision int64 `json:"revision"`
	Total    int64 `json:"total"`
	Pending  int64 `json:"pending"`
}

func (d *DB) ActivityIndexState(ctx context.Context, router string) (ActivityIndexState, error) {
	var s ActivityIndexState
	err := d.sql.QueryRowContext(ctx, `SELECT revision,total,(SELECT COUNT(*) FROM activity_pending WHERE router_id=?) FROM activity_index_state WHERE router_id=?`, router, router).Scan(&s.Revision, &s.Total, &s.Pending)
	if err == sql.ErrNoRows {
		err = nil
	}
	return s, err
}

// shapeOf names exactly the displayed grouping identity. Reception, source key,
// precise timestamp and router log text stay on the original row, not the shape.
func shapeOf(e activity.Event) (activity.Event, string, string, error) {
	e.ID = 0
	e.At = 0
	e.Received = 0
	e.RouterTime = ""
	e.SourceKey = ""
	e.Count = 0
	e.Replay = false
	e.Day = ""
	e.Hour = ""
	e.Country = ""
	e.City = ""
	e.DNSAnswers = []activity.DNSAnswer{}
	if e.DNSIPs == nil {
		e.DNSIPs = []string{}
	}
	if e.Kind == "dns" {
		e.Service = ""
		e.Category = ""
	}
	b, err := json.Marshal(e)
	sum := sha256.Sum256(b)
	return e, string(b), hex.EncodeToString(sum[:]), err
}

type activityIndexer struct {
	tx                       *sql.Tx
	insert, find, fact, done *sql.Stmt
	shapes                   map[string]int64
}

func newActivityIndexer(tx *sql.Tx) (*activityIndexer, error) {
	w := &activityIndexer{tx: tx, shapes: map[string]int64{}}
	var err error
	for _, p := range []struct {
		dst **sql.Stmt
		q   string
	}{
		{&w.insert, `INSERT OR IGNORE INTO activity_shapes(router_id,fingerprint,payload,kind,domain,client,user,ip,mac,server,canonical,device_name,ip_basis,qtype,match_method,destination,label,user_label) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`},
		{&w.find, `SELECT id FROM activity_shapes WHERE router_id=? AND fingerprint=?`},
		{&w.fact, `INSERT OR IGNORE INTO activity_facts(event_id,router_id,at,shape_id) VALUES(?,?,?,?)`},
		{&w.done, `DELETE FROM activity_pending WHERE event_id=?`},
	} {
		*p.dst, err = tx.Prepare(p.q)
		if err != nil {
			w.close()
			return nil, err
		}
	}
	return w, nil
}
func (w *activityIndexer) close() {
	for _, s := range []*sql.Stmt{w.insert, w.find, w.fact, w.done} {
		if s != nil {
			s.Close()
		}
	}
}
func (w *activityIndexer) add(id int64, e activity.Event) error {
	shape, body, fp, err := shapeOf(e)
	if err != nil {
		return err
	}
	key := e.RouterID + ":" + fp
	shapeID := w.shapes[key]
	if shapeID == 0 {
		label := shape.Domain
		if label == "" {
			label = shape.Destination
		}
		user := shape.User
		if user == "" {
			user = shape.MAC
		}
		if user == "" {
			user = shape.IP
		}
		err = w.find.QueryRow(shape.RouterID, fp).Scan(&shapeID)
		if err == sql.ErrNoRows {
			if _, err = w.insert.Exec(shape.RouterID, fp, body, shape.Kind, shape.Domain, shape.Client, shape.User, shape.IP, shape.MAC, shape.Server, shape.Canonical, shape.DeviceName, shape.IPBasis, shape.QType, shape.Match, shape.Destination, label, user); err != nil {
				return err
			}
			if err = w.find.QueryRow(shape.RouterID, fp).Scan(&shapeID); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if len(w.shapes) < 65536 {
			w.shapes[key] = shapeID
		}
	}
	if _, err = w.fact.Exec(id, e.RouterID, e.At, shapeID); err != nil {
		return err
	}
	_, err = w.done.Exec(id)
	return err
}

// IndexActivityBatch backfills from SQLite, never from a router. A bounded
// transaction advances the durable queue only with its corresponding facts.
func (d *DB) IndexActivityBatch(ctx context.Context, limit int) (int, error) {
	if err := d.activityMu.LockContext(ctx); err != nil {
		return 0, err
	}
	defer d.activityMu.Unlock()
	if limit < 1 || limit > 2000 {
		limit = 500
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT e.id,e.router_id,e.at,e.payload FROM activity_pending p JOIN activity_events e ON e.id=p.event_id ORDER BY p.event_id LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	events := []activity.Event{}
	for rows.Next() {
		var e activity.Event
		var b string
		var id, at int64
		var router string
		if err = rows.Scan(&id, &router, &at, &b); err == nil {
			err = json.Unmarshal([]byte(b), &e)
		}
		if err != nil {
			rows.Close()
			return 0, err
		}
		e.ID = id
		e.RouterID = router
		e.At = at
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	w, err := newActivityIndexer(tx)
	if err != nil {
		return 0, err
	}
	defer w.close()
	for _, e := range events {
		if err = ctx.Err(); err != nil {
			return 0, err
		}
		if err = w.add(e.ID, e); err != nil {
			return 0, err
		}
	}
	return len(events), tx.Commit()
}

// PrepareActivityShapes refreshes derived values once per distinct shape/context,
// not once per source event. Caller serializes context changes with queries.
func (d *DB) PrepareActivityShapes(ctx context.Context, router, version, zone string, rules []activity.Rule, lookup func(string) (string, string)) error {
	var previous string
	if err := d.sql.QueryRowContext(ctx, `SELECT version FROM activity_projection_context WHERE router_id=?`, router).Scan(&previous); err != nil && err != sql.ErrNoRows {
		return err
	}
	if previous != version {
		if err := d.resetActivityShapes(ctx, router, previous, version); err != nil {
			return err
		}
	}
	evaluator := activity.NewEvaluator(zone, rules, lookup)
	for {
		rows, err := d.sql.QueryContext(ctx, `SELECT id,payload FROM activity_shapes WHERE router_id=? AND ready=0 LIMIT 100`, router)
		if err != nil {
			return err
		}
		type item struct {
			id int64
			e  activity.Event
		}
		batch := []item{}
		for rows.Next() {
			var it item
			var b string
			if err = rows.Scan(&it.id, &b); err == nil {
				err = json.Unmarshal([]byte(b), &it.e)
			}
			if err != nil {
				rows.Close()
				return err
			}
			evaluator.Classify(&it.e)
			evaluator.Enrich(&it.e)
			batch = append(batch, it)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		tx, err := d.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, it := range batch {
			b, _ := json.Marshal(it.e)
			if _, err = tx.ExecContext(ctx, `UPDATE activity_shapes SET service=?,category=?,country=?,search_text=?,enriched=?,ready=1 WHERE id=?`, it.e.Service, it.e.Category, it.e.Country, activity.SearchText(it.e), string(b), it.id); err != nil {
				break
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM activity_shape_answers WHERE shape_id=?`, it.id); err != nil {
				break
			}
			for _, a := range it.e.DNSAnswers {
				if _, err = tx.ExecContext(ctx, `INSERT INTO activity_shape_answers(shape_id,ip,country) VALUES(?,?,?)`, it.id, a.IP, a.Country); err != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
}

// An interrupted context change must not repeat one large invalidation forever.
// The existing opaque version stores a durable cursor until all old derived rows
// are invalidated. Readers remain in preparation until the final version commits.
type activityShapeReset struct {
	Version string `json:"version"`
	After   int64  `json:"after"`
}

func (d *DB) resetActivityShapes(ctx context.Context, router, previous, version string) error {
	reset := activityShapeReset{Version: version}
	var saved activityShapeReset
	if json.Unmarshal([]byte(previous), &saved) == nil && saved.Version == version && saved.After >= 0 {
		reset = saved
	}
	for {
		tx, err := d.sql.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id FROM activity_shapes WHERE router_id=? AND ready=1 AND id>? ORDER BY id LIMIT 100`, router, reset.After)
		if err != nil {
			tx.Rollback()
			return err
		}
		last, n := reset.After, 0
		for rows.Next() {
			if err = rows.Scan(&last); err != nil {
				break
			}
			n++
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err == nil && n > 0 {
			_, err = tx.ExecContext(ctx, `UPDATE activity_shapes SET ready=0 WHERE router_id=? AND ready=1 AND id>? AND id<=?`, router, reset.After, last)
		}
		next := version
		if n == 100 {
			b, _ := json.Marshal(activityShapeReset{Version: version, After: last})
			next = string(b)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO activity_projection_context VALUES(?,?) ON CONFLICT(router_id) DO UPDATE SET version=excluded.version`, router, next)
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		if n < 100 {
			return nil
		}
		reset.After = last
	}
}

// activityShapeWhere is shared by counts, analysis, Events and export candidates.
// Literal instr over Go-folded text preserves Unicode and '%'/'_' characters.
func activityShapeWhere(f activity.Filter) (string, []any) {
	clauses := []string{}
	args := []any{}
	add := func(q string, v any) { clauses = append(clauses, q); args = append(args, v) }
	for _, p := range []struct{ col, value string }{
		{"kind", f.Kind}, {"client", f.Client}, {"domain", f.Domain}, {"service", f.Service}, {"category", f.Category},
	} {
		if p.value != "" {
			add("s."+p.col+"=?", p.value)
		}
	}
	for _, p := range []struct{ col, value string }{
		{"canonical", f.Canonical}, {"device_name", f.DeviceName}, {"ip_basis", f.IPBasis}, {"server", f.Server}, {"qtype", f.QType}, {"match_method", f.Match},
	} {
		if p.value != "" {
			v := strings.TrimSpace(p.value)
			if v == "-" {
				v = ""
			}
			add("s.kind='dns' AND s."+p.col+"=?", v)
		}
	}
	if f.Country != "" {
		add("s.kind='firewall' AND s.country=?", f.Country)
	}
	if f.DNSIP != "" {
		add("s.kind='dns' AND EXISTS(SELECT 1 FROM activity_shape_answers a WHERE a.shape_id=s.id AND a.ip=?)", f.DNSIP)
	}
	if f.DNSCountry != "" {
		add("s.kind='dns' AND EXISTS(SELECT 1 FROM activity_shape_answers a WHERE a.shape_id=s.id AND a.country=?)", f.DNSCountry)
	}
	if f.Search != "" {
		add("instr(s.search_text,?)>0", strings.ToLower(f.Search))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " AND (" + strings.Join(clauses, ") AND (") + ")", args
}

// ActivityIndexes is used by the local backfill worker, including disabled routers.
func (d *DB) ActivityIndexes(ctx context.Context) (map[string]ActivityIndexState, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT router_id,revision,total,(SELECT COUNT(*) FROM activity_pending p WHERE p.router_id=s.router_id) FROM activity_index_state s`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ActivityIndexState{}
	for rows.Next() {
		var id string
		var v ActivityIndexState
		if err = rows.Scan(&id, &v.Revision, &v.Total, &v.Pending); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}
