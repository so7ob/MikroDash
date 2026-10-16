package db

import (
	"context"
	"encoding/json"
	"mikrodash/internal/activity"
)

// SQLite groups and sorts the full filtered time range; Go retains one page.
// Dynamic service/category filters are evaluated against the current local rules.
func (d *DB) ActivityEventPage(ctx context.Context, router string, f activity.Filter, rules []activity.Rule, lookups ...func(string) (string, string)) ([]activity.Event, int, error) {
	sortExpr := map[string]string{"last": "at", "kind": "kind", "label": "COALESCE(NULLIF(json_extract(payload,'$.domain'),''),json_extract(payload,'$.destination'))", "user": "COALESCE(NULLIF(json_extract(payload,'$.user'),''),NULLIF(json_extract(payload,'$.mac'),''),json_extract(payload,'$.ip'))", "count": "n"}[f.Sort]
	if sortExpr == "" {
		sortExpr = "at"
	}
	dir := " ASC"
	if f.Desc {
		dir = " DESC"
	}
	// Every displayed detail participates, so two rows with different IP/MAC or
	// matching provenance never disappear into one ambiguous group.
	q := `SELECT id,payload,COUNT(*) AS n FROM activity_events WHERE router_id=? AND at>=? AND at<?
 GROUP BY at/1000,kind,json_extract(payload,'$.client'),json_extract(payload,'$.domain'),json_extract(payload,'$.destination'),json_extract(payload,'$.user'),json_extract(payload,'$.server'),json_extract(payload,'$.ip'),json_extract(payload,'$.mac'),json_extract(payload,'$.qtype'),json_extract(payload,'$.match'),json_extract(payload,'$.protocol'),json_extract(payload,'$.in'),json_extract(payload,'$.out'),json_extract(payload,'$.length'),json_extract(payload,'$.timeCertain') ORDER BY ` + sortExpr + dir + `,id DESC`
	cfg, err := d.ActivityState(router)
	if err != nil {
		return nil, 0, err
	}
	var lookup func(string) (string, string)
	if len(lookups) > 0 {
		lookup = lookups[0]
	}
	rows, err := d.sql.QueryContext(ctx, q, router, f.From, f.To)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []activity.Event{}
	total := 0
	start := (f.Page - 1) * f.Limit
	for rows.Next() {
		var e activity.Event
		var b string
		var count int
		var id int64
		if err = rows.Scan(&id, &b, &count); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal([]byte(b), &e); err != nil {
			return nil, 0, err
		}
		e.ID = id
		e.Count = count
		if e.Kind == "dns" {
			e.Service, e.Category = activity.Classify(e.Domain, rules)
		}
		activity.Enrich(&e, cfg.Timezone, lookup)
		if !activity.Matches(e, f) {
			continue
		}
		if total >= start && len(out) < f.Limit {
			out = append(out, e)
		}
		total++
	}
	return out, total, rows.Err()
}
