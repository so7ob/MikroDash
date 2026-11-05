package subscriber

// FinalityIndexes is migration 42. Only final Stop/service-absence rows enter
// these partial indexes; rapid interim accounting gains no extra index writes.
const FinalityIndexes = `
CREATE INDEX IF NOT EXISTS subscriber_final_stop
 ON subscriber_events(lease_id) WHERE status='stop';
CREATE INDEX IF NOT EXISTS subscriber_absence_finality
 ON subscriber_reconciliations(lease_id) WHERE actor='service:subscriber';
CREATE INDEX IF NOT EXISTS subscriber_subscription_account
 ON subscriber_subscriptions(account_id);
`
