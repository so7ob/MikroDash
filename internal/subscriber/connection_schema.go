package subscriber

// ConnectionSchema is additive migration 43. It stores transport/authorization
// context beside existing one-use tickets, never a second balance authority.
// Old tickets have no NAS-bound flow and cannot be redeemed after upgrade.
const ConnectionSchema = `
CREATE TABLE IF NOT EXISTS subscriber_connection_flows (
 token_hash TEXT PRIMARY KEY REFERENCES subscriber_tickets(token_hash) ON DELETE CASCADE,
 router_id TEXT NOT NULL, client_address TEXT NOT NULL, created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL, state TEXT NOT NULL DEFAULT 'requested'
 CHECK(state IN ('requested','authorizing','authorized','rejected')),
 lease_id TEXT REFERENCES subscriber_leases(id), reasons TEXT NOT NULL DEFAULT '[]'
);
`
