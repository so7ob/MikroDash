package subscriber

// ReconciliationSchema is migration 40. Unknown byte distribution is an explicit
// hold; it is never presented as confirmed usage in two calendar periods.
const ReconciliationSchema = `
CREATE TABLE IF NOT EXISTS subscriber_byte_gaps (
 id TEXT PRIMARY KEY, event_key TEXT NOT NULL, lease_id TEXT NOT NULL REFERENCES subscriber_leases(id),
 limit_id TEXT NOT NULL, from_at INTEGER NOT NULL, until_at INTEGER NOT NULL,
 amount INTEGER NOT NULL CHECK(amount>=0), resolved_at INTEGER NOT NULL DEFAULT 0,
 actor TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '', evidence TEXT NOT NULL DEFAULT '',
 allocations TEXT NOT NULL DEFAULT '[]', UNIQUE(event_key,limit_id)
);
CREATE TABLE IF NOT EXISTS subscriber_gap_periods (
 gap_id TEXT NOT NULL REFERENCES subscriber_byte_gaps(id), period_start INTEGER NOT NULL,
 hold_id TEXT NOT NULL UNIQUE REFERENCES subscriber_holds(id), PRIMARY KEY(gap_id,period_start)
);
CREATE TABLE IF NOT EXISTS subscriber_legacy_exposure (
 id TEXT PRIMARY KEY, subscription_id TEXT NOT NULL REFERENCES subscriber_subscriptions(id),
 limit_id TEXT NOT NULL, period_start INTEGER NOT NULL, amount INTEGER NOT NULL CHECK(amount>=0),
 original_quality TEXT NOT NULL, resolved_at INTEGER NOT NULL DEFAULT 0,
 actor TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '', evidence TEXT NOT NULL DEFAULT '',
 settled_amount INTEGER NOT NULL DEFAULT 0,
 UNIQUE(subscription_id,limit_id,period_start)
);
INSERT OR IGNORE INTO subscriber_legacy_exposure(id,subscription_id,limit_id,period_start,amount,original_quality)
 SELECT subscription_id||':'||limit_id||':'||period_start,subscription_id,limit_id,period_start,used,quality
 FROM subscriber_buckets WHERE quality='boundary-conservative';
UPDATE subscriber_buckets SET used=0,quality='legacy-conservative-review' WHERE quality='boundary-conservative';
`
