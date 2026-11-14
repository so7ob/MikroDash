package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mikrodash/internal/activity"
	"strconv"
	"strings"
	"time"
)

// EmptyActivityResult keeps the existing non-null array contract for partial
// responses. Parts is opt-in; old consumers still receive the complete result.
func EmptyActivityResult(f activity.Filter) activity.Result {
	return activity.Result{Filter: f, Rows: []activity.Ranking{}, Events: []activity.Event{}, Services: []activity.Ranking{}, Clients: []activity.Ranking{}, Categories: []activity.Ranking{}, Countries: []activity.Ranking{}, DNSCountries: []activity.Ranking{}, QueryTypes: []activity.Ranking{}, MatchMethods: []activity.Ranking{}, IPBases: []activity.Ranking{}, Series: []activity.Point{}, Heatmap: []activity.Heat{}, Coverage: []activity.Coverage{}, Rules: []activity.Rule{}}
}

// localTimeExpression uses the timezone's real transition intervals, including
// DST and fractional-hour offsets. No per-row Go callback or timezone file read.
func localTimeOffset(column, zone string, from, to int64) string {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	offsets := []string{}
	t := time.UnixMilli(from)
	for t.UnixMilli() < to {
		local := t.In(loc)
		_, offset := local.Zone()
		_, end := local.ZoneBounds()
		if end.IsZero() || end.UnixMilli() >= to {
			offsets = append(offsets, "ELSE "+strconv.Itoa(offset))
			break
		}
		offsets = append(offsets, fmt.Sprintf("WHEN %s<%d THEN %d", column, end.UnixMilli(), offset))
		t = end
	}
	offset := "0"
	if len(offsets) == 1 {
		offset = strings.TrimPrefix(offsets[0], "ELSE ")
	} else if len(offsets) > 1 {
		offset = "CASE " + strings.Join(offsets, " ") + " END"
	}
	return offset
}
func localTimeExpression(column, format, zone string, from, to int64) string {
	return "strftime('" + format + "',(" + column + "/1000)+(" + localTimeOffset(column, zone, from, to) + "),'unixepoch')"
}
func activityCalendarWhere(alias string, f activity.Filter, zone string) (string, []any) {
	q := ""
	args := []any{}
	if f.Day != "" {
		q += " AND " + localTimeExpression(alias+".at", "%Y-%m-%d", zone, f.From, f.To) + "=?"
		args = append(args, f.Day)
	}
	if f.Hour != "" {
		q += " AND " + localTimeExpression(alias+".at", "%Y-%m-%d %H:00", zone, f.From, f.To) + "=?"
		args = append(args, f.Hour)
	}
	return q, args
}

type activitySQLReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (d *DB) activityHistogram(ctx context.Context, router string, f activity.Filter, zone string, out *activity.Result) error {
	return activityHistogramQuery(ctx, d.sql, router, f, zone, out)
}
func activityHistogramQuery(ctx context.Context, read activitySQLReader, router string, f activity.Filter, zone string, out *activity.Result) error {
	histogramFilter := f
	// The chart already counts DNS only; an explicit DNS source filter is
	// redundant and must retain the same bounded boundary-hour plan.
	if histogramFilter.Kind == "dns" {
		histogramFilter.Kind = ""
	}
	shapeSQL, shapeArgs := activityShapeWhere(histogramFilter)
	calSQL, calArgs := activityCalendarWhere("a", f, zone)
	bucket := int64(time.Hour / time.Millisecond)
	if f.To-f.From > 7*24*bucket {
		bucket *= 6
	}
	utcBucket := "a.at/" + strconv.FormatInt(bucket, 10) + "*" + strconv.FormatInt(bucket, 10)
	localBucket := "(a.at/1000+(" + localTimeOffset("a.at", zone, f.From, f.To) + "))/3600"
	wholeHour := activityUsesWholeHours(zone, f.From, f.To)
	// Filter shapes once, then seek their hourly/fact indexes. Aggregate each
	// source before UNION so SQLite never materializes hundreds of thousands
	// of union rows just to discard most of them in an outer filter.
	matching := `WITH matching AS MATERIALIZED (SELECT s.id FROM activity_shapes s WHERE s.router_id=? AND s.kind='dns'` + shapeSQL + `) `
	args := append([]any{router}, shapeArgs...)
	raw := func(from, to int64) string {
		args = append(args, router, from, to)
		args = append(args, calArgs...)
		// Unfiltered partial hours contain few observations. Seek that time
		// range once instead of probing every retained shape twice, including
		// thousands with no events in either boundary hour.
		if shapeSQL == "" && to-from <= 3600000 {
			return `SELECT ` + utcBucket + ` AS bucket,` + localBucket + ` AS localhour,COUNT(*) AS n FROM activity_facts a INDEXED BY activity_facts_time JOIN activity_shapes s ON s.id=a.shape_id WHERE a.router_id=? AND a.at>=? AND a.at<? AND s.kind='dns'` + calSQL + ` GROUP BY 1,2`
		}
		return `SELECT ` + utcBucket + ` AS bucket,` + localBucket + ` AS localhour,COUNT(*) AS n FROM matching m CROSS JOIN activity_facts a INDEXED BY activity_facts_shape ON a.shape_id=m.id WHERE a.router_id=? AND a.at>=? AND a.at<?` + calSQL + ` GROUP BY 1,2`
	}
	lo, hi := (f.From+3600000-1)/3600000, f.To/3600000
	source := ""
	if wholeHour && lo < hi {
		// Hours may be folded into six-hour chart buckets; heatmap keeps every
		// local hour, including the repeated hour at a DST transition.
		hourAt := "h.hour*3600000"
		hb := "(" + hourAt + ")/" + strconv.FormatInt(bucket, 10) + "*" + strconv.FormatInt(bucket, 10)
		lh := "h.hour+(" + localTimeOffset(hourAt, zone, f.From, f.To) + ")/3600"
		if shapeSQL == "" {
			source = `SELECT ` + hb + ` AS bucket,` + lh + ` AS localhour,SUM(h.n) AS n FROM activity_hour_totals h WHERE h.router_id=? AND h.hour>=? AND h.hour<? AND h.kind='dns' GROUP BY 1,2`
		} else {
			source = `SELECT ` + hb + ` AS bucket,` + lh + ` AS localhour,SUM(h.n) AS n FROM matching m CROSS JOIN activity_hourly h INDEXED BY activity_hourly_shape ON h.shape_id=m.id WHERE h.router_id=? AND h.hour>=? AND h.hour<? GROUP BY 1,2`
		}
		source = strings.TrimSuffix(source, " GROUP BY 1,2") + strings.ReplaceAll(calSQL, "a.at", "(h.hour*3600000)") + " GROUP BY 1,2"
		args = append(args, router, lo, hi)
		args = append(args, calArgs...)
		source += " UNION ALL " + raw(f.From, lo*3600000) + " UNION ALL " + raw(hi*3600000, f.To)
	} else {
		source = raw(f.From, f.To)
	}
	q := matching + `SELECT bucket,localhour,SUM(n) FROM (` + source + `) GROUP BY 1,2`
	rows, err := read.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	points := map[int64]int{}
	heats := map[string]int{}
	for rows.Next() {
		var at int64
		var hour int64
		var n int
		if err = rows.Scan(&at, &hour, &n); err != nil {
			rows.Close()
			return err
		}
		points[at] += n
		local := time.Unix(hour*3600, 0).UTC()
		heats[fmt.Sprintf("%d %02d", local.Weekday(), local.Hour())] += n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for at := f.From / bucket * bucket; at < f.To; at += bucket {
		out.Series = append(out.Series, activity.Point{At: at, Count: points[at]})
	}
	for day := 0; day < 7; day++ {
		for hour := 0; hour < 24; hour++ {
			out.Heatmap = append(out.Heatmap, activity.Heat{Day: day, Hour: hour, Count: heats[fmt.Sprintf("%d %02d", day, hour)]})
		}
	}
	return nil
}

type activityCursor struct {
	Key       string `json:"k"`
	Second    int64  `json:"t"`
	Shape     int64  `json:"i"`
	Number    int64  `json:"n"`
	Text      string `json:"s"`
	Back      bool   `json:"b"`
	Inclusive bool   `json:"a"`
}

var ErrActivityCursor = errors.New("invalid activity cursor")

func cursorKey(router string, f activity.Filter) string {
	f.Cursor = ""
	f.Anchor = ""
	f.Page = 0
	f.Limit = 0
	f.Parts = ""
	if f.RollingDays > 0 {
		f.From = 0
		f.To = 0
	}
	body, _ := json.Marshal(struct {
		Router string
		Filter activity.Filter
	}{router, f})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:16])
}
func encodeActivityCursor(c activityCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeActivityCursor(raw, key string) (activityCursor, error) {
	var c activityCursor
	if len(raw) > 4096 {
		return c, ErrActivityCursor
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return c, ErrActivityCursor
	}
	if json.Unmarshal(b, &c) != nil || c.Key != key || c.Shape <= 0 {
		return c, ErrActivityCursor
	}
	return c, nil
}

// activityGroupBase limits boundary groups using their exact raw timestamps.
// Interior groups use the committed count directly. A fractional time range
// never includes an out-of-range source row just because its second overlaps.
func activityGroupBase(router string, f activity.Filter, zone string, whole bool) (string, []any) {
	shapeFilter := f
	shapeFilter.Kind = ""
	shapeSQL, shapeArgs := activityShapeWhere(shapeFilter)
	calF := f
	calSQL, calArgs := activityCalendarWhere("a", calF, zone)
	boundary := `a.router_id=g.router_id AND a.shape_id=g.shape_id AND a.at>=g.second*1000 AND a.at<g.second*1000+1000 AND a.at>=? AND a.at<?` + calSQL
	// Calendar filters are evaluated on facts at boundaries too; date/hour are
	// constant inside a second for RouterOS timestamps and supported IANA zones.
	effective := `CASE WHEN g.first_at>=? AND g.last_at<? THEN g.n ELSE (SELECT COUNT(*) FROM activity_facts a WHERE ` + boundary + `) END`
	args := []any{f.From, f.To, f.From, f.To}
	args = append(args, calArgs...)
	if whole {
		effective = "g.n"
		args = nil
	}
	q := `SELECT g.second,g.shape_id,` + effective + ` AS n,g.first_id,g.kind,g.label,g.user_label FROM activity_event_groups g`
	if shapeSQL != "" {
		q = `SELECT g.second,g.shape_id,` + effective + ` AS n,g.first_id,g.kind,g.label,g.user_label FROM activity_shapes s CROSS JOIN activity_event_groups g INDEXED BY activity_groups_shape ON g.shape_id=s.id AND g.router_id=s.router_id`
	}
	q += ` WHERE g.router_id=? AND g.second>=? AND g.second<=?` + shapeSQL
	args = append(args, router, f.From/1000, (f.To-1)/1000)
	args = append(args, shapeArgs...)
	if f.Kind != "" {
		q += " AND g.kind=?"
		args = append(args, f.Kind)
	}
	if f.Day != "" {
		q += " AND " + localTimeExpression("g.second*1000", "%Y-%m-%d", zone, f.From, f.To) + "=?"
		args = append(args, f.Day)
	}
	if f.Hour != "" {
		q += " AND " + localTimeExpression("g.second*1000", "%Y-%m-%d %H:00", zone, f.From, f.To) + "=?"
		args = append(args, f.Hour)
	}
	return q, args
}
func (d *DB) activityIndexedEvents(ctx context.Context, router string, f activity.Filter, zone string, rules []activity.Rule, lookup func(string) (string, string), countsKey string, expires int64, access ...activitySQLReader) (activity.Result, error) {
	var read activitySQLReader = d.sql
	if len(access) > 0 {
		read = access[0]
	}
	out := EmptyActivityResult(f)
	key := cursorKey(router, f)
	var partial bool
	err := read.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM activity_event_groups WHERE router_id=? AND (second=? OR second=?) AND (first_at<? OR last_at>=?))`, router, f.From/1000, (f.To-1)/1000, f.From, f.To).Scan(&partial)
	if err != nil {
		return out, err
	}
	base, args := activityGroupBase(router, f, zone, !partial)
	if cached, ok := d.activityCache.get(countsKey, time.Now().UnixMilli()); ok {
		out.TotalRows, out.Totals = cached.TotalRows, cached.Totals
	} else {
		if err = d.activityGroupCounts(ctx, router, f, zone, &out, read); err != nil {
			return out, err
		}
		d.activityCache.put(countsKey, out, expires)
	}
	expr := "second"
	numeric := true
	switch f.Sort {
	case "count":
		expr = "n"
	case "kind":
		expr = "kind"
		numeric = false
	case "label":
		expr = "label"
		numeric = false
	case "user":
		expr = "user_label"
		numeric = false
	}
	raw := f.Cursor
	if f.Anchor != "" {
		raw = f.Anchor
	}
	var cursor activityCursor
	if raw != "" {
		cursor, err = decodeActivityCursor(raw, key)
		if err != nil {
			return out, err
		}
		if f.Anchor != "" {
			cursor.Back = false
			cursor.Inclusive = true
			var number int64
			var text string
			if numeric {
				err = read.QueryRowContext(ctx, `SELECT `+expr+` FROM (`+base+`) WHERE second=? AND shape_id=? AND n>0`, append(append([]any{}, args...), cursor.Second, cursor.Shape)...).Scan(&number)
			} else {
				err = read.QueryRowContext(ctx, `SELECT `+expr+` FROM (`+base+`) WHERE second=? AND shape_id=? AND n>0`, append(append([]any{}, args...), cursor.Second, cursor.Shape)...).Scan(&text)
			}
			if err == nil {
				cursor.Number, cursor.Text = number, text
			} else if err != sql.ErrNoRows {
				return out, err
			}
		}
	}
	descending := f.Desc
	if cursor.Back {
		descending = !descending
	}
	direction, op := " ASC", ">"
	if descending {
		direction, op = " DESC", "<"
	}
	q := `SELECT second,shape_id,n,first_id,kind,label,user_label FROM (` + base + `) WHERE n>0`
	if raw != "" {
		// Tie breakers always follow time/shape descending in the forward direction.
		tieOp := "<"
		if cursor.Back {
			tieOp = ">"
		}
		inclusive := tieOp
		if cursor.Inclusive {
			inclusive += "="
		}
		value := any(cursor.Text)
		if numeric {
			value = cursor.Number
		}
		if expr == "second" {
			q += ` AND (second ` + op + ` ? OR (second=? AND shape_id ` + inclusive + ` ?))`
			args = append(args, cursor.Second, cursor.Second, cursor.Shape)
		} else {
			q += ` AND (` + expr + op + `? OR (` + expr + `=? AND (second ` + tieOp + ` ? OR (second=? AND shape_id ` + inclusive + ` ?))))`
			args = append(args, value, value, cursor.Second, cursor.Second, cursor.Shape)
		}
	}
	tieDir := " DESC"
	if cursor.Back {
		tieDir = " ASC"
	}
	q += ` ORDER BY ` + expr + direction
	if expr != "second" {
		q += `,second` + tieDir
	}
	q += `,shape_id` + tieDir + ` LIMIT ?`
	args = append(args, f.Limit+1)
	if raw == "" && f.Page > 1 {
		q += " OFFSET ?"
		args = append(args, (f.Page-1)*f.Limit)
	}
	rows, err := read.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	type group struct {
		second, shape, id int64
		n                 int
		kind, label, user string
	}
	groups := []group{}
	for rows.Next() {
		var g group
		if err = rows.Scan(&g.second, &g.shape, &g.n, &g.id, &g.kind, &g.label, &g.user); err != nil {
			rows.Close()
			return out, err
		}
		groups = append(groups, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	more := len(groups) > f.Limit
	if more {
		groups = groups[:f.Limit]
	}
	out.HasNext = more
	out.HasPrevious = raw != "" || f.Page > 1
	if cursor.Back {
		out.HasPrevious = more
		out.HasNext = true
	}
	if len(groups) == 0 && f.Anchor != "" {
		// Retention can remove the reference and every row following it. Seek
		// back from that tuple to the nearest surviving page instead of blanking.
		fallback := f
		fallback.Anchor = ""
		cursor.Back = true
		cursor.Inclusive = false
		fallback.Cursor = encodeActivityCursor(cursor)
		nearest, err := d.activityIndexedEvents(ctx, router, fallback, zone, rules, lookup, countsKey, expires, read)
		nearest.HasNext = false
		nearest.NextCursor = ""
		nearest.Filter = f
		nearest.Filter.Page = max(1, (nearest.TotalRows+f.Limit-1)/f.Limit)
		return nearest, err
	}
	if cursor.Back {
		for i, j := 0, len(groups)-1; i < j; i, j = i+1, j-1 {
			groups[i], groups[j] = groups[j], groups[i]
		}
	}
	evaluator := activity.NewEvaluator(zone, rules, lookup)
	makeCursor := func(g group, back, inclusive bool) string {
		c := activityCursor{Key: key, Second: g.second, Shape: g.shape, Back: back, Inclusive: inclusive, Number: g.second}
		switch expr {
		case "n":
			c.Number = int64(g.n)
		case "kind":
			c.Text = g.kind
		case "label":
			c.Text = g.label
		case "user_label":
			c.Text = g.user
		}
		return encodeActivityCursor(c)
	}
	for _, g := range groups {
		var b string
		var e activity.Event
		// Use the first retained source within the exact range, not an arbitrary
		// bare GROUP BY column or a source pruned from the leading partial second.
		err = read.QueryRowContext(ctx, `SELECT e.id,e.payload FROM activity_facts a JOIN activity_events e ON e.id=a.event_id WHERE a.router_id=? AND a.shape_id=? AND a.at>=? AND a.at<? ORDER BY a.event_id LIMIT 1`, router, g.shape, max(f.From, g.second*1000), min(f.To, g.second*1000+1000)).Scan(&e.ID, &b)
		if err != nil {
			return out, err
		}
		id := e.ID
		if err = json.Unmarshal([]byte(b), &e); err != nil {
			return out, err
		}
		e.ID = id
		e.Count = g.n
		evaluator.Classify(&e)
		evaluator.Enrich(&e)
		out.Events = append(out.Events, e)
	}
	if len(groups) > 0 {
		out.Anchor = makeCursor(groups[0], false, true)
		if out.HasPrevious {
			out.PrevCursor = makeCursor(groups[0], true, false)
		}
		if out.HasNext {
			out.NextCursor = makeCursor(groups[len(groups)-1], false, false)
		}
	}
	out.GeoAvailable = lookup != nil
	return out, nil
}

// Complete UTC days use daily rollups; their boundary days use complete hours.
// Only the two partial hours walk original facts. Each source carries its exact
// representative timestamp/ID key, avoiding one random seek per result shape.
func activityUsesWholeHours(zone string, from, to int64) bool {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	for at := time.UnixMilli(from); at.UnixMilli() < to; {
		local := at.In(loc)
		_, offset := local.Zone()
		if offset%3600 != 0 {
			return false
		}
		_, end := local.ZoneBounds()
		if end.IsZero() || end.UnixMilli() >= to {
			break
		}
		if end.UnixMilli()%3600000 != 0 {
			return false
		}
		at = end
	}
	return true
}
func activityHourlySource(router string, from, to int64) string {
	return activityRollupSource(router, from, to, false)
}
func activityDailySource(router string, from, to int64) string {
	return activityRollupSource(router, from, to, true)
}
func activityRollupSource(router string, from, to int64, daily bool) string {
	r := "'" + strings.ReplaceAll(router, "'", "''") + "'"
	raw := func(lo, hi int64) string {
		return fmt.Sprintf("SELECT router_id,shape_id,at,1 AS n,evidence_key FROM activity_facts WHERE router_id=%s AND at>=%d AND at<%d", r, lo, hi)
	}
	var parts []string
	hours := func(lo, hi int64) {
		first, last := (lo+3599999)/3600000, hi/3600000
		if first >= last {
			parts = append(parts, raw(lo, hi))
			return
		}
		parts = append(parts, fmt.Sprintf("SELECT router_id,shape_id,last_at AS at,n,evidence_key FROM activity_hourly WHERE router_id=%s AND hour>=%d AND hour<%d", r, first, last))
		if lo < first*3600000 {
			parts = append(parts, raw(lo, first*3600000))
		}
		if last*3600000 < hi {
			parts = append(parts, raw(last*3600000, hi))
		}
	}
	first, last := (from+86399999)/86400000, to/86400000
	if !daily || first >= last {
		hours(from, to)
	} else {
		parts = append(parts, fmt.Sprintf("SELECT router_id,shape_id,last_at AS at,n,evidence_key FROM activity_daily WHERE router_id=%s AND day>=%d AND day<%d", r, first, last))
		hours(from, first*86400000)
		hours(last*86400000, to)
	}
	return strings.Join(parts, " UNION ALL ")
}

func (d *DB) activityFilterChoices(ctx context.Context, router string, out *activity.Result) error {
	// Called only after PrepareActivityShapes completes for this router. Reading
	// the scalar covering index avoids a payload-table seek per retained shape.
	rows, err := d.sql.QueryContext(ctx, `SELECT DISTINCT 'category',category FROM activity_shapes INDEXED BY activity_shapes_summary WHERE router_id=? AND kind='dns'
 UNION SELECT DISTINCT 'country',country FROM activity_shapes INDEXED BY activity_shapes_summary WHERE router_id=? AND kind='firewall'
 UNION SELECT DISTINCT 'dnsCountry',a.country FROM activity_shapes s INDEXED BY activity_shapes_summary CROSS JOIN activity_shape_answers a ON a.shape_id=s.id WHERE s.router_id=?`, router, router, router)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, value string
		if err = rows.Scan(&kind, &value); err != nil {
			return err
		}
		r := activity.Ranking{Key: value, Label: value}
		switch kind {
		case "category":
			out.Categories = append(out.Categories, r)
		case "country":
			out.Countries = append(out.Countries, r)
		case "dnsCountry":
			out.DNSCountries = append(out.DNSCountries, r)
		}
	}
	return rows.Err()
}

// Count complete UTC days from transactional rollups. Only the two boundary
// fragments need per-second groups; count cost does not grow with page depth.
func (d *DB) activityGroupCounts(ctx context.Context, router string, f activity.Filter, zone string, out *activity.Result, access ...activitySQLReader) error {
	var read activitySQLReader = d.sql
	if len(access) > 0 {
		read = access[0]
	}
	ranges := [][2]int64{{f.From, f.To}}
	calendar := f.Day != "" || f.Hour != ""
	if !calendar || activityUsesWholeHours(zone, f.From, f.To) {
		shapeFilter := f
		shapeFilter.Kind = ""
		shapeSQL, _ := activityShapeWhere(shapeFilter)
		if shapeSQL == "" {
			lo, hi := (f.From+3599999)/3600000, f.To/3600000
			if lo < hi {
				q := `SELECT COALESCE(SUM(groups),0),COALESCE(SUM(CASE WHEN kind='dns' THEN n ELSE 0 END),0),COALESCE(SUM(CASE WHEN kind='firewall' THEN n ELSE 0 END),0) FROM activity_hour_totals h WHERE router_id=? AND hour>=? AND hour<?`
				args := []any{router, lo, hi}
				cal, ca := activityCalendarWhere("a", f, zone)
				q += strings.ReplaceAll(cal, "a.at", "(h.hour*3600000)")
				args = append(args, ca...)
				if f.Kind != "" {
					q += " AND kind=?"
					args = append(args, f.Kind)
				}
				if err := read.QueryRowContext(ctx, q, args...).Scan(&out.TotalRows, &out.Totals.DNS, &out.Totals.Firewall); err != nil {
					return err
				}
				ranges = [][2]int64{{f.From, lo * 3600000}, {hi * 3600000, f.To}}
			}
		} else {
			buckets := []struct {
				table, column string
				width         int64
			}{{"activity_daily", "day", 86400000}, {"activity_hourly", "hour", 3600000}}
			if calendar {
				buckets = buckets[1:]
			}
			for _, bucket := range buckets {
				var partial [][2]int64
				for _, r := range ranges {
					lo, hi := (r[0]+bucket.width-1)/bucket.width, r[1]/bucket.width
					if lo >= hi {
						partial = append(partial, r)
						continue
					}
					where, values := activityShapeWhere(f)
					q := `SELECT COALESCE(SUM(g.groups),0),COALESCE(SUM(CASE WHEN s.kind='dns' THEN g.n ELSE 0 END),0),COALESCE(SUM(CASE WHEN s.kind='firewall' THEN g.n ELSE 0 END),0) FROM activity_shapes s CROSS JOIN ` + bucket.table + ` g INDEXED BY ` + bucket.table + `_shape ON g.router_id=s.router_id AND g.shape_id=s.id WHERE s.router_id=? AND g.` + bucket.column + `>=? AND g.` + bucket.column + `<?` + where
					cal, ca := activityCalendarWhere("a", f, zone)
					q += strings.ReplaceAll(cal, "a.at", "(g.hour*3600000)")
					values = append(values, ca...)
					var groups, dns, firewall int
					if err := read.QueryRowContext(ctx, q, append([]any{router, lo, hi}, values...)...).Scan(&groups, &dns, &firewall); err != nil {
						return err
					}
					out.TotalRows += groups
					out.Totals.DNS += dns
					out.Totals.Firewall += firewall
					partial = append(partial, [2]int64{r[0], lo * bucket.width}, [2]int64{hi * bucket.width, r[1]})
				}
				ranges = partial
			}
		}
	}
	for _, r := range ranges {
		if r[0] >= r[1] {
			continue
		}
		boundary := f
		boundary.From = r[0]
		boundary.To = r[1]
		base, args := activityGroupBase(router, boundary, zone, false)
		var groups, dns, firewall int
		if err := read.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN kind='dns' THEN n ELSE 0 END),0),COALESCE(SUM(CASE WHEN kind='firewall' THEN n ELSE 0 END),0) FROM (`+base+`) WHERE n>0`, args...).Scan(&groups, &dns, &firewall); err != nil {
			return err
		}
		out.TotalRows += groups
		out.Totals.DNS += dns
		out.Totals.Firewall += firewall
	}
	return nil
}
