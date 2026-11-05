package subscriber

// SessionSchema is migration 39. Operation intent and observations survive an
// application restart independently of the transport's acknowledgement.
const SessionSchema = `
CREATE TABLE IF NOT EXISTS subscriber_operations (
 id TEXT PRIMARY KEY, lease_id TEXT NOT NULL REFERENCES subscriber_leases(id),
 kind TEXT NOT NULL, state TEXT NOT NULL, created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0,
 target_deadline INTEGER NOT NULL, previous_deadline INTEGER NOT NULL,
 request TEXT NOT NULL, packet TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS subscriber_one_operation ON subscriber_operations(lease_id)
 WHERE state IN ('requested','applying','acknowledged');
CREATE TABLE IF NOT EXISTS subscriber_observations (
 lease_id TEXT PRIMARY KEY REFERENCES subscriber_leases(id), router_id TEXT NOT NULL,
 observed_at INTEGER NOT NULL, present INTEGER NOT NULL, payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS subscriber_sources (
 lease_id TEXT NOT NULL REFERENCES subscriber_leases(id), source TEXT NOT NULL,
 observed_at INTEGER NOT NULL, PRIMARY KEY(lease_id,source)
);
CREATE TABLE IF NOT EXISTS subscriber_holds (
 id TEXT PRIMARY KEY, lease_id TEXT NOT NULL REFERENCES subscriber_leases(id),
 limit_id TEXT NOT NULL, period_start INTEGER NOT NULL, amount INTEGER NOT NULL CHECK(amount>=0),
 resolved_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS subscriber_reconciliations (
 id TEXT PRIMARY KEY, lease_id TEXT NOT NULL REFERENCES subscriber_leases(id),
 actor TEXT NOT NULL, reason TEXT NOT NULL, evidence TEXT NOT NULL,
 created_at INTEGER NOT NULL, seconds INTEGER NOT NULL, download INTEGER NOT NULL,
 upload INTEGER NOT NULL
);
`
