# Subscriber feature inventory

Snapshot: 2026-11-13 (Asia/Aden). Baseline `ba39515f003484211afe3dfef783ade6dc39f813` on `develop`; `main` = `ca2b1d5284d2da98967a5bc482282ee85d02bc8e`. Issue #95, parent #38. Source inspection is not an executed test or physical-device acceptance.

The repository contains 2,278 tracked files. Inspection mapped all top-level runtime subsystems, subscriber Go storage/policy/protocol/workers, HTTP/portal handlers, TypeScript forms, schema integration, RBAC, generators, CI and related test entry points. This initial audit does not claim line-by-line execution of every file. All Go paths below are under `internal/subscriber/` unless explicit.

| ID | Requirement group | State | Code evidence | Existing tests (execution recorded separately) | Remaining acceptance | Priority |
|---|---|---|---|---|---|---|
| A01 | Create password/voucher account; edit display name; suspend/reactivate; reviewed atomic status batches | Partial | storage.go: CreateAccount, SetStatus; account_name.go; account_status_batch.go | accounting_test.go; account_name_test.go; account_status_batch_test.go | Password reset, credential management, subscriber detail route, ownership grouping and complete history absent | P0 |
| A02 | Search, sorting, filtering, import/export, linked devices and status | Partial | storage.go: Accounts (unbounded SELECT); web/src/pages/subscribers.ts: rows; connections.go | web/test/subscriber-account-actions.test.ts; connection_test.go | Client-side full-account filtering; no paged account API, full export or detail hub | P1 |
| B01 | Immutable plans, time/directional/total limits, calendar resets, windows, multiple speeds, FUP | Partial | policy.go: Plan.Validate, Evaluate, Period.Bounds; storage.go: SavePlan | calendar_test.go; calendar_delegation_test.go; accounting_test.go | Backend supports core intersections; draft UI lacks full editing; decimal leading-zero bug in base; no archive/copy workflow | P0 |
| C01 | PAP Access-Accept/Reject, Start/Interim/Stop, shared admission and duplicate accounting | Implemented but Not Hardware Verified | radius.go; radius_receipt.go; accounting.go; receipts.go | radius_test.go; receipts_recovery_test.go; accounting_test.go | Pending Hardware Verification: L009/C; CHR is historical scoped lab evidence only | P0 |
| C02 | Durable CoA, Disconnect, bounded delegation, exact-target readback, watcher-independent enforcement | Implemented but Not Hardware Verified | coa.go; operations.go; runtime.go; internal/server/subscriber_observer.go | operations_test.go; disconnect_test.go; renewal_budget_test.go | Pending Hardware Verification: rate/timeout/direction, outage and resource measurements on each target | P0 |
| C03 | Missing Stop, receipts, period gaps and administrative settlement | Implemented but Not Hardware Verified | reconciliation.go; receipt_coverage.go; legacy_exposure.go; finality_schema.go | final_accounting_test.go; reconciliation_test.go; receipts_recovery_test.go | Physical target retransmission/restart acceptance pending; operator approvals must remain explicit | P0 |
| D01 | HotSpot managed admission and isolated portal handoff | Partial | internal/server/subscriber_portal.go; subscriber_login_routing.go; subscriber/landing.go | internal/server/subscriber_proxy_integration_test.go; subscriber_handoff_browser_test.go | Pending Hardware Verification on L009/C; captive OS coverage incomplete | P1 |
| D02 | Independent PPPoE service with PPP authentication and IP assignment | Missing | radius.go rejects CHAP; NAS/observer/readiness/landing model is HotSpot-specific | No independent PPPoE acceptance suite found | Explicit service type, PAP/CHAP decision, PPP observation, fixed/dynamic IP policies and isolated tests needed | P1 |
| E01 | Single voucher and batch inventory, delivery, print/QR, CSV/PDF, reseller allocation | Partial | storage.go: CreateAccount; subscribers.go: create; no batch schema/API found | accounting_test.go covers voucher authentication | Only single secret returned once; no durable recoverable batch delivery. #94 closed not_planned; no remote/local voucher branch found | P1 |
| F01 | Global active sessions, freshness/rates, scope-bound disconnect, live admin updates | Partial | storage.go: Snapshot; observations.go; internal/server/subscriber_socket.go | connection_test.go; subscriber_connection_socket_test.go | Owned portal sessions exist; global filtered/paged admin session surface and safe command endpoint missing | P1 |
| G01 | Assign/renew and first-login activation with historical plan references | Partial | storage.go: Assign, subscription; delegation.go; renewal.go (network delegation only) | accounting_test.go; final_operations_test.go | Commercial Assign lacks operation idempotency, receipt, extension/top-up/rollover; no conflation with network renewal | P1 |
| H01 | Optional billing invoices/payments/currencies/refunds and financial audit | Missing | No subscriber invoice/payment/financial ledger tables/actions in storage.go or server/subscribers.go | No subscriber billing acceptance suite found | Build balanced auditable financial documents separately in existing SQLite, after entitlement operations | P2 |
| I01 | Reseller scope, vouchers, balances, commissions and limits | Missing | rbac provides system:subscribers; no reseller ownership in subscriber_accounts | subscriber_offline_test.go verifies global authority only | Row-level ownership, scoped queries/mutations and isolation tests must precede reseller UI | P2 |
| J01 | Subscriber events, delivery log, retries and deduplicated notifications | Missing | policy.go alert produces reason only; existing internal/alertdispatch, mailer, notify are reusable | No subscriber event-outbox acceptance suite found | Reuse dispatcher transports with transactional outbox, unique delivery keys and retry ownership | P2 |
| K01 | So7ob Arabic self-service portal, balances, sessions, speeds, QR entry | Partial | web/src/subscriber-login.ts; web/src/subscriber-view.ts; internal/server/subscriber_portal.go | web/test/subscriber-login.test.ts; subscriber-view.test.ts; internal/server/subscriber_browser_test.go | Credential changes, renewal/recharge, receipts and subscription history require backend contracts | P2 |
| L01 | User Manager read-only preview and migration | Partial | readiness.go: PreviewUserManager; cmd/subscribercheck | No transactional importer suite found; #79 adds preview validation | No approved mapping/current-period opening balance rules, chunked apply/resume/rollback or idempotent import | P1 |
| M01 | Subscriber/business/RADIUS reports and scaled exports | Missing | Existing internal/reports and reportpdf; no subscriber analytics report API | No 100/1k/10k/100k subscriber measurement evidence found | Aggregate indexed SQL, bounded exports and explicit measurement quality needed | P2 |
| N01 | NAS capability/config validation and read-only readiness | Partial | runtime.go: Config.Validate; readiness.go: Inspect; cmd/subscribercheck; internal/guard/subscriber* | runtime/proxy/login-routing and guard tests | No complete admin setup/site/backup workflow; flags cannot be enabled from CHR assumptions | P2 |

Allowed states: Implemented & Verified; Implemented but Not Hardware Verified; Partial; Missing; Broken; Blocked; Not Applicable. No complete A-N axis is accepted solely from test-file presence.

## Delivery delta from the inspected baseline

P0 input validation is delivered through #77 (149123b7). Structured draft limits
are delivered through #78 (b29ee7d6), with protected HTTP/SQLite persistence and
immutable assignment proof added in this session. Paged status history is delivered
through #80 (ca6d5e95) and #81 (4f1828c7), including readable stored batch fields,
original evidence disclosure and a genuine HTTP/SQLite browser journey. These
close narrow acceptance checkpoints, not full A/B/C axes. Import preview #79 is
reviewed and awaiting final delivery gates. The current detailed states and test
contracts are in FEATURE-MATRIX.md; exact tested heads and CI are in TEST-EVIDENCE.md.

The owner selected fresh commercial periods without rollover, retaining all prior
subscriptions and usage. Commercial idempotency/receipts, credential rotation,
full account audit, full policy editing, server pagination, voucher delivery, PPPoE
and transactional migration remain open. The status-history benchmark covers
100/1k/10k/100k synthetic account populations only; it is not whole-platform load
or physical NAS acceptance.
