# Blockers, missing work and resolved decisions

Snapshot: 2026-11-13 (Asia/Aden). Missing software is backlog work, not an excuse
to mark the whole project blocked. Independent implementation continues while
physical acceptance awaits its external requirements.

| ID | Class / actual condition | Required resolution / independent work |
|---|---|---|
| HW-C | #76 L009/C target acceptance remains unperformed in this session | Refresh version/target/source/capability evidence and client availability; prepare scoped commands/preflight/rollback, obtain separate live authority and execute real traffic/auth/accounting/CoA/disconnect/outage tests. Continue software work. |
| VERIFY-SCRATCH | Ignored local Go scratch is not gofmt-clean | Use an exact-SHA clean verification worktree; preserve the owner's scratch. Full unchanged gates have passed there. |
| VOUCHER-REF | #94 closed not_planned; requested feature ref absent in accessible local/remote refs and reflogs; voucher PR search found unrelated single-voucher/portal work | Create a new scoped issue, reusing existing account/plan primitives. No recovery of unknown lost code is claimed. |

Resolved: the old SQLite dependency restriction no longer blocks this environment.
Fresh store/HTTP tests execute. The owner explicitly selected fresh periods without
rollover, preserving historical usage/subscriptions; commercial fulfillment no
longer awaits that choice. Final CI waits for #79 and the audit are delivery gates,
not completed acceptance. Original #77-#81 branches are preserved.

Still Missing/Partial: credentials and portal/ticket revocation; broader audit;
idempotent commercial operations/receipts; full plan states/windows/speeds/archive;
server-paged directory/detail; durable atomic voucher delivery/print/inventory;
admin live sessions; PPPoE; migration apply/rollback; financial/reseller/outbox/report
modules; staged setup/restore; whole-platform concurrency/load and physical tests.
These are named in FEATURE-MATRIX and DEVELOPMENT-ROADMAP. No main promotion,
payment gateway, deployment, router write or production cutover is authorized.
