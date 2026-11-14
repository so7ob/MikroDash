package db

import (
	"context"
	"database/sql"
	"mikrodash/internal/activity"
	"modernc.org/sqlite"
	"strconv"
	"strings"
	"time"
)

func init() {
	sqlite.MustRegisterCollationUtf8("activity_fold", func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
}

// The temporary work table contains shape totals, never decoded raw payloads.
// All ranking distinct counts, grouping, ordering and pagination stay in SQLite.
// A dedicated connection keeps the temporary table private for this snapshot.
func (d *DB) activitySQLAggregate(ctx context.Context, router string, f activity.Filter, rules []activity.Rule, zone string, lookup func(string) (string, string), staged ...activityAggregateRequest) (out activity.Result, err error) {
	job := &activityAggregateJob{result: EmptyActivityResult(f)}
	if len(staged) > 0 {
		if d.activityJob != nil && d.activityJob.key == staged[0].Key && time.Now().UnixMilli() < d.activityJob.expires {
			job = d.activityJob
		} else {
			job.key = staged[0].Key
			job.expires = time.Now().Add(2 * time.Minute).UnixMilli()
			d.activityJob = job
		}
	}
	out = job.result
	f = out.Filter
	out.GeoAvailable = lookup != nil
	full := f.Parts == "" || f.Parts == "overview"
	conn, err := d.sql.Conn(ctx)
	if err != nil {
		return out, err
	}
	defer conn.Close()
	keep := false
	defer func() {
		if !keep {
			conn.ExecContext(context.Background(), `DELETE FROM activity_query_work; DELETE FROM activity_query_shapes; DELETE FROM activity_query_current; DELETE FROM activity_query_answers`)
			if len(staged) > 0 {
				d.activityJob = nil
			}
		}
	}()
	for {
		didWork := true
		switch job.stage {
		case 0:
			ready := true
			err = func() error {
				tx, e := conn.BeginTx(ctx, nil)
				if e != nil {
					return e
				}
				defer tx.Rollback()
				if len(staged) > 0 {
					if e = tx.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM activity_shapes WHERE router_id=? AND ready=0) AND NOT EXISTS(SELECT 1 FROM activity_pending WHERE router_id=?)`, router, router).Scan(&ready); e != nil {
						return e
					}
					if !ready {
						return nil
					}
					if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0),COALESCE(MAX(total),0) FROM activity_index_state WHERE router_id=?`, router).Scan(&out.DataRevision, &out.TotalStored); e != nil {
						return e
					}
					out.ComputedAt = time.Now().UnixMilli()
				}
				if _, err = tx.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS activity_query_work(shape_id INTEGER,at INTEGER,id INTEGER,n INTEGER,calendar INTEGER); DROP INDEX IF EXISTS activity_query_work_calendar; CREATE INDEX IF NOT EXISTS activity_query_work_time ON activity_query_work(at DESC,id DESC);
 CREATE TEMP TABLE IF NOT EXISTS activity_query_shapes(id INTEGER PRIMARY KEY,kind TEXT,domain TEXT,service TEXT,category TEXT,user TEXT,server TEXT,ip TEXT,mac TEXT,client TEXT,canonical TEXT,device_name TEXT,ip_basis TEXT,qtype TEXT,match_method TEXT,country TEXT);
 CREATE TEMP TABLE IF NOT EXISTS activity_query_current(shape_id INTEGER PRIMARY KEY,at INTEGER,n INTEGER);
 CREATE TEMP TABLE IF NOT EXISTS activity_query_answers(shape_id INTEGER,ip TEXT,country TEXT,PRIMARY KEY(shape_id,ip)) WITHOUT ROWID;
 DELETE FROM activity_query_current; DELETE FROM activity_query_answers; DELETE FROM activity_query_work; DELETE FROM activity_query_shapes;`); err != nil {
					return err
				}
				from := f.From
				if full {
					from -= f.To - f.From
				}
				shapeSQL, shapeArgs := activityShapeWhere(f)
				rf := f
				rf.From = from
				calSQL, calArgs := activityCalendarWhere("a", rf, zone)
				group := "a.shape_id,(a.at<" + strconv.FormatInt(f.From, 10) + ")"
				source, count := "activity_facts", "COUNT(*)"
				calendar := f.Day != "" || f.Hour != "" || f.View == "domains" && (f.Group == "day" || f.Group == "hour")
				if !calendar || activityUsesWholeHours(zone, from, f.To) {
					rollup := activityDailySource
					if calendar {
						rollup = activityHourlySource
					}
					source = rollup(router, f.From, f.To)
					if full {
						source = rollup(router, from, f.From) + " UNION ALL " + source
					}
					source = "(" + source + ")"
					count = "SUM(a.n)"
				}
				bucket := "NULL"
				if f.View == "domains" && (f.Group == "day" || f.Group == "hour") {
					width := int64(86400)
					if f.Group == "hour" {
						width = 3600
					}
					local := "(a.at/1000+(" + localTimeOffset("a.at", zone, from, f.To) + "))"
					bucket = "(" + local + "/" + strconv.FormatInt(width, 10) + ")-(" + local + "%" + strconv.FormatInt(width, 10) + "<0)"

					group += ",5"
				}

				query := `INSERT INTO activity_query_work SELECT a.shape_id,MAX(a.at),activity_evidence_id(MAX(a.evidence_key)),` + count + `,` + bucket + `
 FROM ` + source + ` a WHERE a.router_id=? AND a.at>=? AND a.at<?`
				args := []any{router, from, f.To}
				// The source branches already enforce router scope. An unfiltered query
				// needs no list of every retained shape or membership probe per bucket.
				if shapeSQL != "" {
					query += ` AND a.shape_id IN (SELECT s.id FROM activity_shapes s WHERE s.router_id=?` + shapeSQL + `)`
					args = append(args, router)
					args = append(args, shapeArgs...)
				}
				query += calSQL + ` GROUP BY ` + group
				args = append(args, calArgs...)
				if _, err = tx.ExecContext(ctx, query, args...); err != nil {
					return err
				}

				if full {
					if e = activityHistogramQuery(ctx, tx, router, f, zone, &out); e != nil {
						return e
					}
				}
				if f.View == "events" {
					page, e := d.activityIndexedEvents(ctx, router, f, zone, rules, lookup, "aggregate-events:"+job.key+":"+strconv.FormatInt(time.Now().UnixNano(), 10), time.Now().Add(time.Minute).UnixMilli(), tx)
					if e != nil {
						return e
					}
					out.Events, out.TotalRows = page.Events, page.TotalRows
					out.Anchor, out.NextCursor, out.PrevCursor = page.Anchor, page.NextCursor, page.PrevCursor
					out.HasNext, out.HasPrevious = page.HasNext, page.HasPrevious
				}
				var rows int
				if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_query_work`).Scan(&rows); e != nil {
					return e
				}
				job.large = len(staged) > 0 && rows > 4096
				return tx.Commit()
			}()
			if err != nil {
				return out, err
			}
			if !ready {
				return activityAggregatePending(f), nil
			}
		case 1:
			if _, err = conn.ExecContext(ctx, `INSERT INTO activity_query_shapes
 SELECT id,kind,domain,service,category,user,server,ip,mac,client,canonical,device_name,ip_basis,qtype,match_method,country
 FROM activity_shapes INDEXED BY activity_shapes_summary WHERE router_id=? AND id IN (SELECT shape_id FROM activity_query_work)`, router); err != nil {
				return out, err
			}

			if _, err = conn.ExecContext(ctx, `INSERT INTO activity_query_answers SELECT a.shape_id,a.ip,a.country FROM activity_shape_answers a WHERE a.shape_id IN (SELECT id FROM activity_query_shapes)`); err != nil {
				return out, err
			}
			var missing bool
			if err = conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM activity_query_work q LEFT JOIN activity_query_shapes s ON s.id=q.shape_id WHERE s.id IS NULL)`).Scan(&missing); err != nil {
				return out, err
			}
			if missing {
				// Retention can collect an unreferenced shape between requests.
				// Restart from current sources instead of dropping its observations.
				return activityAggregatePending(f), nil
			}

			if f.View == "domains" && (f.Group == "day" || f.Group == "hour") {
				if _, err = conn.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS activity_query_work_calendar ON activity_query_work(calendar,at DESC,id DESC) WHERE calendar IS NOT NULL`); err != nil {
					return out, err
				}
			}
		case 2:
			if _, err = conn.ExecContext(ctx, `INSERT INTO activity_query_current SELECT shape_id,MAX(at),SUM(n) FROM activity_query_work WHERE at>=? GROUP BY shape_id`, f.From); err != nil {
				return out, err
			}
			current := ` FROM activity_query_current q JOIN activity_query_shapes s ON s.id=q.shape_id WHERE q.at>=?`
			totals := `SELECT COALESCE(SUM(CASE WHEN s.kind='dns' THEN q.n ELSE 0 END),0),COALESCE(SUM(CASE WHEN s.kind='firewall' THEN q.n ELSE 0 END),0),
 COUNT(DISTINCT CASE WHEN s.kind='dns' THEN s.domain END),COUNT(DISTINCT CASE WHEN s.kind='dns' AND s.user<>'' THEN s.client END),
 COUNT(DISTINCT CASE WHEN s.kind='dns' AND s.user='' THEN s.client END),COUNT(DISTINCT CASE WHEN s.kind='dns' THEN s.client END),
 COUNT(DISTINCT CASE WHEN s.kind='dns' THEN NULLIF(s.device_name,'') END),COUNT(DISTINCT CASE WHEN s.kind='dns' THEN NULLIF(s.canonical,'') END),
 COALESCE(SUM(CASE WHEN s.kind='dns' AND EXISTS(SELECT 1 FROM activity_query_answers z WHERE z.shape_id=s.id) THEN q.n ELSE 0 END),0)` + current
			t := &out.Totals
			if err = conn.QueryRowContext(ctx, totals, f.From).Scan(&t.DNS, &t.Firewall, &t.Domains, &t.Users, &t.Unmapped, &t.Sources, &t.DeviceNames, &t.Canonicals, &t.Answered); err != nil {
				return out, err
			}
			if err = conn.QueryRowContext(ctx, `SELECT COUNT(DISTINCT z.ip) FROM activity_query_current q JOIN activity_query_shapes s ON s.id=q.shape_id JOIN activity_query_answers z ON z.shape_id=s.id WHERE q.at>=? AND s.kind='dns'`, f.From).Scan(&t.AnswerIPs); err != nil {
				return out, err
			}

			if full {
				if err = conn.QueryRowContext(ctx, `SELECT COALESCE(SUM(q.n),0) FROM activity_query_work q JOIN activity_query_shapes s ON s.id=q.shape_id WHERE q.at<? AND s.kind='dns'`, f.From).Scan(&out.Previous); err != nil {
					return out, err
				}

			}
		case 3, 4, 5, 6:
			didWork = full
			if full {
				start := (job.stage - 3) * 2
				end := start + 2
				for _, r := range []struct {
					key   string
					limit int
					dst   *[]activity.Ranking
				}{
					{"service", 10, &out.Services}, {"client", 10, &out.Clients}, {"category", 0, &out.Categories}, {"country", 0, &out.Countries}, {"dnsCountry", 0, &out.DNSCountries}, {"qtype", 0, &out.QueryTypes}, {"match", 0, &out.MatchMethods}, {"ipBasis", 0, &out.IPBases},
				}[start:end] {
					if r.key == "country" && out.Totals.Firewall == 0 || r.key != "country" && out.Totals.DNS == 0 {
						continue
					}
					*r.dst, _, err = activityRankSQL(ctx, conn, f, zone, r.key, r.limit, false)
					if err != nil {
						return out, err
					}
				}
			}
		case 7:
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

			if _, err = conn.ExecContext(ctx, `DELETE FROM activity_query_work; DELETE FROM activity_query_shapes; DELETE FROM activity_query_current; DELETE FROM activity_query_answers`); err != nil {
				return out, err
			}
			conn.Close()
			return out, nil
		}
		job.stage++
		job.result = out
		if job.large && didWork {
			keep = true
			return activityAggregatePending(f), nil
		}
	}
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
		join = " JOIN activity_query_answers z ON z.shape_id=s.id"
	case "dnsIP":
		expr = "z.ip"
		join = " JOIN activity_query_answers z ON z.shape_id=s.id"
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
		expr = "q.calendar"
		extra = " AND q.calendar IS NOT NULL"
	}
	if !paged || !(client && f.Sort == "label") {
		return activityFacetSQL(ctx, conn, f, expr, extra, join, client, missing, limit, paged)
	}
	cte := `WITH evidence AS (SELECT ` + expr + ` AS key,q.n,q.at,s.category,s.service,s.user,s.server,s.ip,s.mac,s.domain,
 ROW_NUMBER() OVER(PARTITION BY ` + expr + ` ORDER BY q.at DESC,q.id DESC) AS position
 FROM activity_query_work q JOIN activity_query_shapes s ON s.id=q.shape_id` + join + ` WHERE q.at>=?` + extra + `),
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
func activityFacetSQL(ctx context.Context, conn *sql.Conn, f activity.Filter, expr, extra, join string, client, missing bool, limit int, paged bool) ([]activity.Ranking, int, error) {
	cte := `WITH grouped AS (SELECT ` + expr + ` AS key,SUM(q.n) AS count,COUNT(DISTINCT NULLIF(s.domain,'')) AS domains,
 COUNT(DISTINCT NULLIF(COALESCE(NULLIF(s.mac,''),s.ip),'')) AS devices,MAX(q.at) AS last,
 CASE WHEN MIN(s.category)=MAX(s.category) THEN MIN(s.category) ELSE 'Mixed' END AS category,
 CASE WHEN MIN(s.service)=MAX(s.service) THEN MIN(s.service) ELSE '' END AS service
 FROM activity_query_work q JOIN activity_query_shapes s ON s.id=q.shape_id` + join + ` WHERE q.at>=?` + extra + ` GROUP BY ` + expr + `)`
	grouped := cte
	order := "count DESC,key ASC"
	if paged {
		column := "count"
		switch f.Sort {
		case "label":
			column = "key COLLATE activity_fold"
			if missing {
				column = "(CASE WHEN key='-' THEN 'Not provided' ELSE key END) COLLATE activity_fold"
			}
		case "category", "domains", "devices", "last":
			column = f.Sort
		}
		direction := " ASC"
		if f.Desc {
			direction = " DESC"
		}
		order = column + direction + ",key ASC"
	}
	cte += `, ranked AS (SELECT *,COUNT(*) OVER() AS total FROM grouped ORDER BY ` + order
	args := []any{f.From}
	if limit > 0 {
		cte += ` LIMIT ?`
		args = append(args, limit)
	}
	if paged && f.Page > 1 {
		cte += ` OFFSET ?`
		args = append(args, (f.Page-1)*f.Limit)
	}
	cte += `) `
	keyOut := `g.key`
	if expr == "q.calendar" {
		format, width := "%Y-%m-%d", 86400
		if f.Group == "hour" {
			format, width = "%Y-%m-%d %H:00", 3600
		}
		keyOut = "strftime('" + format + "',g.key*" + strconv.Itoa(width) + ",'unixepoch')"
	}
	label := keyOut
	if client {
		label = `COALESCE(NULLIF(e.user,''),NULLIF(e.mac,''),e.ip)`
	}
	if missing {
		label = `CASE WHEN g.key='-' THEN 'Not provided' ELSE g.key END`
	}
	query := cte + `SELECT ` + keyOut + `,` + label + `,g.service,g.category,e.user,e.server,e.ip,e.mac,g.count,g.domains,g.devices,g.last,g.total
 FROM ranked g JOIN activity_query_shapes e ON e.id=(SELECT q.shape_id FROM activity_query_work q JOIN activity_query_shapes s ON s.id=q.shape_id` + join + `
 WHERE q.at=g.last AND ` + expr + `=g.key` + extra + ` ORDER BY q.id DESC LIMIT 1) ORDER BY ` + strings.ReplaceAll(order, "category", "g.category")
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
		if err = conn.QueryRowContext(ctx, grouped+` SELECT COUNT(*) FROM grouped`, f.From).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	return result, total, nil
}
