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

func (d *DB) activityHistogram(ctx context.Context, router string, f activity.Filter, zone string, out *activity.Result) error {
	shapeSQL, shapeArgs := activityShapeWhere(f)
	calSQL, calArgs := activityCalendarWhere("a", f, zone)
	bucket := int64(time.Hour / time.Millisecond)
	if f.To-f.From > 7*24*bucket {
		bucket *= 6
	}
	utcBucket := "a.at/" + strconv.FormatInt(bucket, 10) + "*" + strconv.FormatInt(bucket, 10)
	localBucket := "(a.at/1000+(" + localTimeOffset("a.at", zone, f.From, f.To) + "))/3600"
	loc, locErr := time.LoadLocation(zone)
	if locErr != nil {
		loc = time.UTC
	}
	wholeHour := true
	for at := f.From; at < f.To; at += 1800000 {
		_, offset := time.UnixMilli(at).In(loc).Zone()
		if offset%3600 != 0 {
			wholeHour = false
			break
		}
	}
	for at := time.UnixMilli(f.From); at.UnixMilli() < f.To; {
		_, end := at.In(loc).ZoneBounds()
		if end.IsZero() || end.UnixMilli() >= f.To {
			break
		}
		if end.UnixMilli()%3600000 != 0 {
			wholeHour = false
			break
		}
		at = end
	}
	// Filter shapes once, then seek their hourly/fact indexes. Aggregate each
	// source before UNION so SQLite never materializes hundreds of thousands
	// of union rows just to discard most of them in an outer filter.
	matching := `WITH matching AS MATERIALIZED (SELECT s.id FROM activity_shapes s WHERE s.router_id=? AND s.kind='dns'` + shapeSQL + `) `
	args := append([]any{router}, shapeArgs...)
	raw := func(from, to int64) string {
		args = append(args, router, from, to)
		args = append(args, calArgs...)
		return `SELECT ` + utcBucket + ` AS bucket,` + localBucket + ` AS localhour,COUNT(*) AS n FROM matching m CROSS JOIN activity_facts a INDEXED BY activity_facts_shape ON a.shape_id=m.id WHERE a.router_id=? AND a.at>=? AND a.at<?` + calSQL + ` GROUP BY 1,2`
	}
	lo, hi := (f.From+3600000-1)/3600000, f.To/3600000
	source := ""
	if wholeHour && f.Day == "" && f.Hour == "" && lo < hi {
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
		args = append(args, router, lo, hi)
		source += " UNION ALL " + raw(f.From, lo*3600000) + " UNION ALL " + raw(hi*3600000, f.To)
	} else {
		source = raw(f.From, f.To)
	}
	q := matching + `SELECT bucket,localhour,SUM(n) FROM (` + source + `) GROUP BY 1,2`
	rows, err := d.sql.QueryContext(ctx, q, args...)
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
	shapeSQL, shapeArgs := activityShapeWhere(f)
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
func (d *DB) activityIndexedEvents(ctx context.Context, router string, f activity.Filter, zone string, rules []activity.Rule, lookup func(string) (string, string), countsKey string, expires int64) (activity.Result, error) {
	out := EmptyActivityResult(f)
	key := cursorKey(router, f)
	var partial bool
	err := d.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM activity_event_groups WHERE router_id=? AND (second=? OR second=?) AND (first_at<? OR last_at>=?))`, router, f.From/1000, (f.To-1)/1000, f.From, f.To).Scan(&partial)
	if err != nil {
		return out, err
	}
	base, args := activityGroupBase(router, f, zone, !partial)
	if cached, ok := d.activityCache.get(countsKey, time.Now().UnixMilli()); ok {
		out.TotalRows, out.Totals = cached.TotalRows, cached.Totals
	} else {
		if err = d.activityGroupCounts(ctx, router, f, zone, &out); err != nil {
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
				err = d.sql.QueryRowContext(ctx, `SELECT `+expr+` FROM (`+base+`) WHERE second=? AND shape_id=? AND n>0`, append(append([]any{}, args...), cursor.Second, cursor.Shape)...).Scan(&number)
			} else {
				err = d.sql.QueryRowContext(ctx, `SELECT `+expr+` FROM (`+base+`) WHERE second=? AND shape_id=? AND n>0`, append(append([]any{}, args...), cursor.Second, cursor.Shape)...).Scan(&text)
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
	rows, err := d.sql.QueryContext(ctx, q, args...)
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
		nearest, err := d.activityIndexedEvents(ctx, router, fallback, zone, rules, lookup, countsKey, expires)
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
		err = d.sql.QueryRowContext(ctx, `SELECT e.id,e.payload FROM activity_facts a JOIN activity_events e ON e.id=a.event_id WHERE a.router_id=? AND a.shape_id=? AND a.at>=? AND a.at<? ORDER BY a.event_id LIMIT 1`, router, g.shape, max(f.From, g.second*1000), min(f.To, g.second*1000+1000)).Scan(&e.ID, &b)
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

// Daily rollups cover complete UTC days; only boundary days walk indexed facts.
// Router values are quoted as SQL literals here, never identifiers or fragments.
func activityDailySource(router string, from, to int64) string {
	r := "'" + strings.ReplaceAll(router, "'", "''") + "'"
	lo, hi := (from+86400000-1)/86400000, to/86400000
	if lo >= hi {
		return fmt.Sprintf("SELECT router_id,shape_id,at,1 AS n FROM activity_facts WHERE router_id=%s AND at>=%d AND at<%d", r, from, to)
	}
	return fmt.Sprintf("SELECT router_id,shape_id,last_at AS at,n FROM activity_daily WHERE router_id=%s AND day>=%d AND day<%d UNION ALL SELECT router_id,shape_id,at,1 AS n FROM activity_facts WHERE router_id=%s AND at>=%d AND at<%d UNION ALL SELECT router_id,shape_id,at,1 AS n FROM activity_facts WHERE router_id=%s AND at>=%d AND at<%d", r, lo, hi, r, from, lo*86400000, r, hi*86400000, to)
}

func (d *DB) activityFilterChoices(ctx context.Context, router string, out *activity.Result) error {
	rows, err := d.sql.QueryContext(ctx, `SELECT DISTINCT 'category',category FROM activity_shapes WHERE router_id=? AND ready=1 AND kind='dns'
 UNION SELECT DISTINCT 'country',country FROM activity_shapes WHERE router_id=? AND ready=1 AND kind='firewall'
 UNION SELECT DISTINCT 'dnsCountry',a.country FROM activity_shape_answers a JOIN activity_shapes s ON s.id=a.shape_id WHERE s.router_id=? AND s.ready=1`, router, router, router)
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
func (d *DB) activityGroupCounts(ctx context.Context, router string, f activity.Filter, zone string, out *activity.Result) error {
	ranges := [][2]int64{{f.From, f.To}}
	lo, hi := (f.From+86400000-1)/86400000, f.To/86400000
	if lo < hi && f.Day == "" && f.Hour == "" {
		shapeSQL, args := activityShapeWhere(f)
		query := `SELECT COALESCE(SUM(g.groups),0),COALESCE(SUM(CASE WHEN s.kind='dns' THEN g.n ELSE 0 END),0),COALESCE(SUM(CASE WHEN s.kind='firewall' THEN g.n ELSE 0 END),0) FROM activity_shapes s CROSS JOIN activity_daily g INDEXED BY activity_daily_shape ON g.router_id=s.router_id AND g.shape_id=s.id WHERE s.router_id=? AND g.day>=? AND g.day<?` + shapeSQL
		if err := d.sql.QueryRowContext(ctx, query, append([]any{router, lo, hi}, args...)...).Scan(&out.TotalRows, &out.Totals.DNS, &out.Totals.Firewall); err != nil {
			return err
		}
		ranges = [][2]int64{{f.From, lo * 86400000}, {hi * 86400000, f.To}}
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
		if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN kind='dns' THEN n ELSE 0 END),0),COALESCE(SUM(CASE WHEN kind='firewall' THEN n ELSE 0 END),0) FROM (`+base+`) WHERE n>0`, args...).Scan(&groups, &dns, &firewall); err != nil {
			return err
		}
		out.TotalRows += groups
		out.Totals.DNS += dns
		out.Totals.Firewall += firewall
	}
	return nil
}
