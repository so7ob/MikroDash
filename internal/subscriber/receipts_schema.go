package subscriber

// ReceiptSchema is migration 41. Acknowledged accounting can be durably pending
// without inventing a zero for an omitted statistic or releasing a delegation.
const ReceiptSchema = `
CREATE TABLE IF NOT EXISTS subscriber_accounting_receipts (
 id TEXT PRIMARY KEY,
 lease_id TEXT REFERENCES subscriber_leases(id),
 router_id TEXT NOT NULL, state TEXT NOT NULL,
 received_at INTEGER NOT NULL, resolved_at INTEGER NOT NULL DEFAULT 0,
 payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS subscriber_receipts_pending
 ON subscriber_accounting_receipts(lease_id,state,received_at);
`
