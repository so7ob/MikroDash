# Architecture review

Snapshot: 2026-11-13 (Asia/Aden). Baseline `ba39515f003484211afe3dfef783ade6dc39f813` on `develop`; `main` = `ca2b1d5284d2da98967a5bc482282ee85d02bc8e`. Issue #95, parent #38. Source inspection is not an executed test or physical-device acceptance.

Keep Go, framework-free TypeScript, one existing SQLite authority, Binary API, RADIUS and WebSocket. `internal/db/db.go` initializes subscriber schema in the existing history/audit database; `internal/subscriber/storage.go` owns accounts, versioned plans, subscriptions, leases, reservations, usage buckets and audits. The immutable `(plan_id, plan_version)` foreign key and current-subscription unique index preserve history.

Authentication and commercial entitlement are distinct: suspended/expired accounts can inspect their portal while network Reserve fails closed. RADIUS Class binds the lease to router/network/device; accounting high-water marks and durable receipts prevent duplicated debit. API observations enter that same ledger. `renewal.go` renews network delegations, not commercial subscription credit. `Assign` supersedes the previous subscription only after outstanding leases have been reconciled.

Background enforcement belongs to RadiusService.Run, not a visible page. Observers use the existing shared RouterOS session/cache/limiter. CoA ACK is acknowledged; only exact fresh queue/session readback establishes applied policy. Capability evidence is target/version-qualified.

Extend through scoped store methods and existing protected handlers, declared WebSocket events and generated payloads. New commercial operations require durable request identities and receipts; new financial tables remain in this SQLite database and must not debit usage buckets. Notifications should reuse mailer/notify transport behind a transactional outbox. Reseller ownership belongs in persisted relations and server predicates before UI exposure.

The current `Accounts` and `Plans` endpoints read entire collections. Separate bounded directory/session queries and aggregate metrics from individual account snapshots. Add query-plan/scale evidence before claiming 100,000-account readiness. New detail routes must retain current page permission keys and navigation event vocabulary.

No migration can replace historical totals with current-period balances. Migration needs a reviewed source fingerprint, explicit mapping/conversion, per-chunk idempotency and rollback that never rewrites already-accounted network usage.
