package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mikrodash/internal/activity"
	"strconv"
	"time"
)

const activityCacheBudget = 32 << 20

type activityCachedResult struct {
	body    []byte
	expires int64
	used    uint64
}
type activityQueryCache struct {
	entries map[string]activityCachedResult
	bytes   int
	clock   uint64
}

func (c *activityQueryCache) get(key string, now int64) (activity.Result, bool) {
	entry, ok := c.entries[key]
	if !ok || now >= entry.expires {
		return activity.Result{}, false
	}
	var out activity.Result
	if json.Unmarshal(entry.body, &out) != nil {
		return out, false
	}
	c.clock++
	entry.used = c.clock
	c.entries[key] = entry
	return out, true
}
func (c *activityQueryCache) put(key string, out activity.Result, expires int64) {
	body, err := json.Marshal(out)
	if err != nil || len(body) > activityCacheBudget/4 {
		return
	}
	if c.entries == nil {
		c.entries = map[string]activityCachedResult{}
	}
	if old, ok := c.entries[key]; ok {
		c.bytes -= len(old.body)
		delete(c.entries, key)
	}
	for c.bytes+len(body) > activityCacheBudget || len(c.entries) >= 128 {
		oldest := ""
		var tick uint64 = ^uint64(0)
		for k, v := range c.entries {
			if v.used < tick {
				oldest = k
				tick = v.used
			}
		}
		c.bytes -= len(c.entries[oldest].body)
		delete(c.entries, oldest)
	}
	c.clock++
	c.entries[key] = activityCachedResult{body: body, expires: expires, used: c.clock}
	c.bytes += len(body)
}

// ActivityIndexedQuery returns ready=false while the durable backfill is still
// pending. Legacy full-history reads remain the fallback, never partial counts.
// Rules/GeoIP are context keys; auth and source status are added by the handler,
// after this cache, on every request.
func (d *DB) ActivityIndexedQuery(ctx context.Context, router string, f activity.Filter, rules []activity.Rule, zone, geoKey string, lookup func(string) (string, string)) (activity.Result, bool, error) {
	if err := d.activityMu.LockContext(ctx); err != nil {
		return activity.Result{}, false, err
	}
	defer d.activityMu.Unlock()
	out := EmptyActivityResult(f)
	state, err := d.ActivityIndexState(ctx, router)
	if err != nil {
		return out, false, err
	}
	out.TotalStored = state.Total
	out.DataRevision = state.Revision
	out.IndexPending = state.Pending
	if f.Parts == "status" || f.Parts == "rules" {
		out.ComputedAt = time.Now().UnixMilli()
		return out, true, nil
	}
	if state.Pending > 0 {
		return out, false, nil
	}
	contextBytes, _ := json.Marshal(struct {
		Rules, Builtins []activity.Rule
		Projection      int
		Zone, Geo       string
	}{rules, activity.Builtins, 1, zone, geoKey})
	digest := sha256.Sum256(contextBytes)
	version := hex.EncodeToString(digest[:])
	// Preparation is durable maintenance, not an incomplete analytical result.
	// Bound each request so slower containers can commit batches and retry without
	// exhausting the HTTP deadline. Never cache or aggregate a partial projection.
	prepareCtx, cancelPrepare := context.WithTimeout(ctx, 2*time.Second)
	err = d.PrepareActivityShapes(prepareCtx, router, version, zone, rules, lookup)
	preparationExpired := prepareCtx.Err() == context.DeadlineExceeded
	cancelPrepare()
	if err != nil {
		if !preparationExpired || ctx.Err() != nil {
			return out, false, err
		}
		if err = d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_shapes WHERE router_id=? AND (ready=0 OR COALESCE((SELECT version FROM activity_projection_context WHERE router_id=?),'')<>?)`, router, router, version).Scan(&out.PreparationPending); err != nil {
			return out, false, err
		}
		out.Preparing = true
		out.ComputedAt = time.Now().UnixMilli()
		out.RefreshAt = out.ComputedAt + 1000
		return out, true, nil
	}
	cacheFilter := f
	if f.RollingDays > 0 {
		cacheFilter.From = 0
		cacheFilter.To = 0
	}
	filterBytes, _ := json.Marshal(cacheFilter)
	key := router + "\x00" + strconv.FormatInt(state.Revision, 10) + "\x00" + version + "\x00" + string(filterBytes)
	now := time.Now().UnixMilli()
	if cached, ok := d.activityCache.get(key, now); ok {
		return cached, true, nil
	}
	expires, err := d.activityExpiry(ctx, router, f, now)
	if err != nil {
		return out, true, err
	}
	countsFilter := cacheFilter
	countsFilter.Parts = ""
	countsFilter.Cursor = ""
	countsFilter.Anchor = ""
	countsFilter.Page = 0
	countsFilter.Limit = 0
	countsFilter.Sort = ""
	countsFilter.Desc = false
	countsBytes, _ := json.Marshal(countsFilter)
	countsKey := "counts:" + router + "\x00" + strconv.FormatInt(state.Revision, 10) + "\x00" + version + "\x00" + string(countsBytes)
	if f.View == "events" && f.Parts == "table" {
		out, err = d.activityIndexedEvents(ctx, router, f, zone, rules, lookup, countsKey, expires)
	} else {
		out, err = d.activitySQLAggregate(ctx, router, f, rules, zone, lookup)
		if err == nil && f.View == "events" {
			page, e := d.activityIndexedEvents(ctx, router, f, zone, rules, lookup, countsKey, expires)
			err = e
			out.Events, out.TotalRows = page.Events, page.TotalRows
			out.Anchor, out.NextCursor, out.PrevCursor = page.Anchor, page.NextCursor, page.PrevCursor
			out.HasNext, out.HasPrevious = page.HasNext, page.HasPrevious
		}
	}
	if err != nil {
		return out, true, err
	}
	if f.Parts == "table" {
		if err = d.activityFilterChoices(ctx, router, &out); err != nil {
			return out, true, err
		}
	}
	out.TotalStored = state.Total
	out.DataRevision = state.Revision
	out.IndexPending = 0
	out.ComputedAt = now
	out.ResultVersion = version + ":" + strconv.FormatInt(state.Revision, 10)
	out.RefreshAt = max(now+1, expires)
	d.activityCache.put(key, out, out.RefreshAt)
	return out, true, nil
}

func (d *DB) activityExpiry(ctx context.Context, router string, f activity.Filter, now int64) (int64, error) {
	expires := now + 60000
	if f.RollingDays > 0 {
		// New data invalidates by revision. In its absence, refresh exactly when a
		// record leaves either rolling window or the time chart gains another hour.
		span := f.To - f.From
		for _, window := range []struct{ from, to, age int64 }{{f.From, f.To, span}, {f.From - span, f.From, span * 2}} {
			var first *int64
			if e := d.sql.QueryRowContext(ctx, `SELECT MIN(at) FROM activity_events WHERE router_id=? AND at>=? AND at<?`, router, window.from, window.to).Scan(&first); e != nil {
				return 0, e
			}
			if first != nil {
				expires = min(expires, *first+window.age+1)
			}
		}
		var upcoming *int64
		if err := d.sql.QueryRowContext(ctx, `SELECT MIN(at) FROM activity_events WHERE router_id=? AND at>=?`, router, f.To).Scan(&upcoming); err != nil {
			return 0, err
		}
		if upcoming != nil {
			expires = min(expires, *upcoming)
		}
		expires = min(expires, (now/3600000+1)*3600000)
	}

	return max(now+1, expires), nil
}
