package db

import (
	"context"
	"database/sql"
	"mikrodash/internal/activity"
	"modernc.org/sqlite"
	"strconv"
	"strings"
)

func init() {
	sqlite.MustRegisterCollationUtf8("activity_fold", func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
}

// The temporary work table contains shape totals, never decoded raw payloads.
// All ranking distinct counts, grouping, ordering and pagination stay in SQLite.
// A dedicated connection keeps the temporary table private for this snapshot.
func (d *DB) activitySQLAggregate(ctx context.Context, router string, f activity.Filter, rules []activity.Rule, zone string, lookup func(string) (string, string)) (activity.Result, error) {
	out := EmptyActivityResult(f)
	out.GeoAvailable = lookup != nil
	conn, err := d.sql.Conn(ctx)
	if err != nil {
		return out, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS activity_query_work(shape_id INTEGER,at INTEGER,id INTEGER,n INTEGER); CREATE INDEX IF NOT EXISTS activity_query_work_time ON activity_query_work(at DESC,id DESC); DELETE FROM activity_query_work;`); err != nil {
		return out, err
	}
	defer conn.ExecContext(context.Background(), `DELETE FROM activity_query_work`)
	from := f.From - (f.To - f.From)
	shapeSQL, shapeArgs := activityShapeWhere(f)
	rf := f
	rf.From = from
	calSQL, calArgs := activityCalendarWhere("a", rf, zone)
	group := "a.shape_id,(a.at<" + strconv.FormatInt(f.From, 10) + ")"
	source, count := "activity_facts", "COUNT(*)"
	if f.Day == "" && f.Hour == "" && !(f.View == "domains" && (f.Group == "day" || f.Group == "hour")) {
		source = "(" + activityDailySource(router, from, f.From) + " UNION ALL " + activityDailySource(router, f.From, f.To) + ")"
		count = "SUM(a.n)"
	}
	if f.View == "domains" && (f.Group == "day" || f.Group == "hour") {
		format := "%Y-%m-%d"
		if f.Group == "hour" {
			format = "%Y-%m-%d %H:00"
		}
		group += "," + localTimeExpression("a.at", format, zone, from, f.To)
	}
	query := `INSERT INTO activity_query_work SELECT shape_id,at,(SELECT MAX(x.event_id) FROM activity_facts x WHERE x.router_id=? AND x.shape_id=g.shape_id AND x.at=g.at),n FROM
 (SELECT a.shape_id,MAX(a.at) AS at,` + count + ` AS n FROM ` + source + ` a WHERE a.router_id=? AND a.at>=? AND a.at<? AND a.shape_id IN (SELECT s.id FROM activity_shapes s WHERE s.router_id=?` + shapeSQL + `)` + calSQL + ` GROUP BY ` + group + `) g`
	args := []any{router, router, from, f.To, router}
	args = append(args, shapeArgs...)
	args = append(args, calArgs...)
	if _, err = conn.ExecContext(ctx, query, args...); err != nil {
		return out, err
	}
	current := ` FROM activity_query_work q JOIN activity_shapes s ON s.id=q.shape_id WHERE q.at>=?`
	totals := `SELECT COALESCE(SUM(CASE WHEN s.kind='dns' THEN q.n ELSE 0 END),0),COALESCE(SUM(CASE WHEN s.kind='firewall' THEN q.n ELSE 0 END),0),
 COUNT(DISTINCT CASE WHEN s.kind='dns' THEN s.domain END),COUNT(DISTINCT CASE WHEN s.kind='dns' AND s.user<>'' THEN s.client END),
 COUNT(DISTINCT CASE WHEN s.kind='dns' AND s.user='' THEN s.client END),COUNT(DISTINCT CASE WHEN s.kind='dns' THEN s.client END),
 COUNT(DISTINCT CASE WHEN s.kind='dns' THEN NULLIF(s.device_name,'') END),COUNT(DISTINCT CASE WHEN s.kind='dns' THEN NULLIF(s.canonical,'') END),
 COALESCE(SUM(CASE WHEN s.kind='dns' AND EXISTS(SELECT 1 FROM activity_shape_answers z WHERE z.shape_id=s.id) THEN q.n ELSE 0 END),0)` + current
	t := &out.Totals
	if err = conn.QueryRowContext(ctx, totals, f.From).Scan(&t.DNS, &t.Firewall, &t.Domains, &t.Users, &t.Unmapped, &t.Sources, &t.DeviceNames, &t.Canonicals, &t.Answered); err != nil {
		return out, err
	}
	if err = conn.QueryRowContext(ctx, `SELECT COUNT(DISTINCT z.ip) FROM activity_query_work q JOIN activity_shapes s ON s.id=q.shape_id JOIN activity_shape_answers z ON z.shape_id=s.id WHERE q.at>=? AND s.kind='dns'`, f.From).Scan(&t.AnswerIPs); err != nil {
		return out, err
	}
	full := f.Parts == "" || f.Parts == "overview"
	if full {
		if err = conn.QueryRowContext(ctx, `SELECT COALESCE(SUM(q.n),0) FROM activity_query_work q JOIN activity_shapes s ON s.id=q.shape_id WHERE q.at<? AND s.kind='dns'`, f.From).Scan(&out.Previous); err != nil {
			return out, err
		}
	}
	// Return facets only where displayed. Table responses do not build hidden charts.
	if full {
		for _, r := range []struct {
			key   string
			limit int
			dst   *[]activity.Ranking
		}{{"service", 10, &out.Services}, {"client", 10, &out.Clients}, {"category", 0, &out.Categories}, {"country", 0, &out.Countries}, {"dnsCountry", 0, &out.DNSCountries}, {"qtype", 0, &out.QueryTypes}, {"match", 0, &out.MatchMethods}, {"ipBasis", 0, &out.IPBases}} {
			*r.dst, _, err = activityRankSQL(ctx, conn, f, zone, r.key, r.limit, false)
			if err != nil {
				return out, err
			}
		}
	}
	key := ""
	switch f.View {
	case "summary", "domains":
		key = f.Group
		if key == "" {
			key = "service"
		}
	case "clients":
		key = "client"
	}
	if key != "" {
		out.Rows, out.TotalRows, err = activityRankSQL(ctx, conn, f, zone, key, f.Limit, true)
		if err != nil {
			return out, err
		}
	}
	// Release before the independent histogram query uses the connection pool.
	if _, err = conn.ExecContext(ctx, `DELETE FROM activity_query_work`); err != nil {
		return out, err
	}
	conn.Close()
	if full {
		err = d.activityHistogram(ctx, router, f, zone, &out)
	}
	return out, err
}

func activityRankSQL(ctx context.Context, conn *sql.Conn, f activity.Filter, zone, key string, limit int, paged bool) ([]activity.Ranking, int, error) {
	expr, extra, join := "s.service", " AND s.kind='dns'", ""
	missing, client := false, false
	switch key {
	case "domain":
		expr = "s.domain"
	case "client":
		expr = "s.client"
		client = true
	case "category":
		expr = "s.category"
	case "country":
		expr = "s.country"
		extra = " AND s.kind='firewall'"
	case "dnsCountry":
		expr = "z.country"
		join = " JOIN activity_shape_answers z ON z.shape_id=s.id"
	case "dnsIP":
		expr = "z.ip"
		join = " JOIN activity_shape_answers z ON z.shape_id=s.id"
	case "canonical":
		expr = "s.canonical"
		extra += " AND s.canonical<>''"
	case "deviceName":
		expr = "s.device_name"
		extra += " AND s.device_name<>''"
	case "server":
		expr = "COALESCE(NULLIF(s.server,''),'-')"
		missing = true
	case "qtype":
		expr = "COALESCE(NULLIF(s.qtype,''),'-')"
		missing = true
	case "match":
		expr = "COALESCE(NULLIF(s.match_method,''),'-')"
		missing = true
	case "ipBasis":
		expr = "COALESCE(NULLIF(s.ip_basis,''),'-')"
		missing = true
	case "day", "hour":
		format := "%Y-%m-%d"
		if key == "hour" {
			format = "%Y-%m-%d %H:00"
		}
		expr = localTimeExpression("q.at", format, zone, f.From, f.To)
		extra = ""
	}
	if !paged {
		return activityFacetSQL(ctx, conn, f, expr, extra, join, client, missing, limit)
	}
	cte := `WITH evidence AS (SELECT ` + expr + ` AS key,q.n,q.at,s.category,s.service,s.user,s.server,s.ip,s.mac,s.domain,
 ROW_NUMBER() OVER(PARTITION BY ` + expr + ` ORDER BY q.at DESC,q.id DESC) AS position
 FROM activity_query_work q JOIN activity_shapes s ON s.id=q.shape_id` + join + ` WHERE q.at>=?` + extra + `),
 grouped AS (SELECT key,SUM(n) AS count,COUNT(DISTINCT NULLIF(domain,'')) AS domains,
 COUNT(DISTINCT NULLIF(COALESCE(NULLIF(mac,''),ip),'')) AS devices,MAX(at) AS last,
 CASE WHEN MIN(category)=MAX(category) THEN MIN(category) ELSE 'Mixed' END AS category,
 CASE WHEN MIN(service)=MAX(service) THEN MIN(service) ELSE '' END AS service,
 MAX(CASE WHEN position=1 THEN user END) AS user,MAX(CASE WHEN position=1 THEN server END) AS server,
 MAX(CASE WHEN position=1 THEN ip END) AS ip,MAX(CASE WHEN position=1 THEN mac END) AS mac FROM evidence GROUP BY key),
 labeled AS (SELECT *,`
	label := "key"
	if client {
		label = "COALESCE(NULLIF(user,''),NULLIF(mac,''),ip)"
	}
	if missing {
		label = "CASE WHEN key='-' THEN 'Not provided' ELSE key END"
	}
	cte += label + ` AS label FROM grouped) `
	order := "count DESC,key ASC"
	if paged {
		expr := "count"
		switch f.Sort {
		case "label":
			expr = "label COLLATE activity_fold"
		case "category", "domains", "devices", "last":
			expr = f.Sort
		}
		direction := " ASC"
		if f.Desc {
			direction = " DESC"
		}
		order = expr + direction + ",key ASC"
	}
	query := cte + `SELECT key,label,service,category,user,server,ip,mac,count,domains,devices,last,COUNT(*) OVER() FROM labeled ORDER BY ` + order
	args := []any{f.From}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	if paged && f.Page > 1 {
		query += " OFFSET ?"
		args = append(args, (f.Page-1)*f.Limit)
	}
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []activity.Ranking{}
	total := 0
	for rows.Next() {
		var r activity.Ranking
		if err = rows.Scan(&r.Key, &r.Label, &r.Service, &r.Category, &r.User, &r.Server, &r.IP, &r.MAC, &r.Count, &r.Domains, &r.Devices, &r.Last, &total); err != nil {
			return nil, 0, err
		}
		if client {
			r.Service, r.Category = "", ""
		}
		result = append(result, r)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	if paged && len(result) == 0 && f.Page > 1 {
		if err = conn.QueryRowContext(ctx, cte+`SELECT COUNT(*) FROM labeled`, f.From).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	return result, total, nil
}

// Chart facets order by count/key. Aggregate once, limit the groups, then seek
// only each returned group's latest evidence instead of sorting every evidence
// row through a window for every chart. Keep the complete Ranking contract.
func activityFacetSQL(ctx context.Context, conn *sql.Conn, f activity.Filter, expr, extra, join string, client, missing bool, limit int) ([]activity.Ranking, int, error) {
	cte := `WITH grouped AS (SELECT ` + expr + ` AS key,SUM(q.n) AS count,COUNT(DISTINCT NULLIF(s.domain,'')) AS domains,
 COUNT(DISTINCT NULLIF(COALESCE(NULLIF(s.mac,''),s.ip),'')) AS devices,MAX(q.at) AS last,
 CASE WHEN MIN(s.category)=MAX(s.category) THEN MIN(s.category) ELSE 'Mixed' END AS category,
 CASE WHEN MIN(s.service)=MAX(s.service) THEN MIN(s.service) ELSE '' END AS service
 FROM activity_query_work q JOIN activity_shapes s ON s.id=q.shape_id` + join + ` WHERE q.at>=?` + extra + ` GROUP BY ` + expr + `),
 ranked AS (SELECT *,COUNT(*) OVER() AS total FROM grouped ORDER BY count DESC,key ASC`
	args := []any{f.From}
	if limit > 0 {
		cte += ` LIMIT ?`
		args = append(args, limit)
	}
	cte += `) `
	label := `g.key`
	if client {
		label = `COALESCE(NULLIF(e.user,''),NULLIF(e.mac,''),e.ip)`
	}
	if missing {
		label = `CASE WHEN g.key='-' THEN 'Not provided' ELSE g.key END`
	}
	query := cte + `SELECT g.key,` + label + `,g.service,g.category,e.user,e.server,e.ip,e.mac,g.count,g.domains,g.devices,g.last,g.total
 FROM ranked g JOIN activity_shapes e ON e.id=(SELECT q.shape_id FROM activity_query_work q JOIN activity_shapes s ON s.id=q.shape_id` + join + `
 WHERE q.at=g.last AND ` + expr + `=g.key` + extra + ` ORDER BY q.id DESC LIMIT 1) ORDER BY g.count DESC,g.key ASC`
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []activity.Ranking{}
	total := 0
	for rows.Next() {
		var r activity.Ranking
		if err = rows.Scan(&r.Key, &r.Label, &r.Service, &r.Category, &r.User, &r.Server, &r.IP, &r.MAC, &r.Count, &r.Domains, &r.Devices, &r.Last, &total); err != nil {
			return nil, 0, err
		}
		if client {
			r.Service, r.Category = "", ""
		}
		result = append(result, r)
	}
	return result, total, rows.Err()
}
