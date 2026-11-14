# Resume checkpoint and next implementation

Work in /root/So7ob/MikroDash. Inspect status/remote/worktrees and fetch current
develop before changing anything; preserve .vscode/, debug binaries, operator
scratch/data and existing feature/checkpoint branches. Read CONTRIBUTING.md,
PROGRESS and TEST-EVIDENCE. Issue #95 records this audit's own delivery SHA/CI.
Do not repeat already passed gates without a new change or unresolved failure.

Current integration: #77, #78, #80, #81 and #79 are delivered, with successful
full local and PR/post-merge CI. Issue #95 tracks the audit PR and its own exact
head/CI. Keep #38/#76 open; no release or runtime activation is implied.

P0-CREDENTIAL is delivered through #99/#100 at develop 45ead3ff, with
local/PR/post-merge checks recorded on #99. #101 adds current administrative
account/subscription audit; its exact delivery head and CI are recorded on #101.
The contract below is retained as implemented acceptance, not a task to restart.
Audited administrator credential rotation provides a reviewed administrator UI/API with modern authority, Origin/CSRF, explicit account
and reason, stale credential-version fencing and scrypt using existing primitives.
Commit new credential/version, audit and outstanding-ticket revocation atomically.
Portal HTTP and WebSocket sessions must reject revoked epochs, including an
in-flight login/reset race; RADIUS admission must not accept stale authentication
after reset. Preserve commercial entitlement, usage and existing network session
history. No secret readback/logging. Test old/new credentials, concurrent resets,
login/admission races, audit/storage rollback, sessions/tickets, unauthorized calls,
input retention/uncertain response and actual protected HTTP/SQLite browser UX.
Define the credential epoch contract before editing network admission code.

P0-AUDIT administrative chronology is implemented and verified in #101: indexed
creation/name/status/credential/owned-subscription/speed events, server filters,
exact safe evidence, readonly authority and shared responsive modal. Preserve it
and the delivered status/credential contracts. Network/session/accounting and
future financial event history still belong to their retained roadmap scopes.

Next independent P0 is commercial operation idempotency. Search existing issues
first and create/reuse a numbered branch from then-current develop. The
owner-approved policy is a new period without rollover. Review must show previous
remaining balance, transferred amount zero and new period credit; retain prior
subscription and usage records. New request keys produce one immutable operation
receipt; identical retries return it, conflicting retries refuse, and concurrent
renewal/restart faults cannot duplicate entitlement. Reconcile active/uncertain
accounting before renewal; distinguish network delegation from commercial credit.

Then finish structured policy states/windows/speeds/copy/archive and P1 directory/
detail with server pagination, recoverable voucher batches, live sessions,
transactional import and independent PPPoE. For large vouchers, stage bounded
chunks without making partial stock usable; publish the complete reviewed batch
atomically, retain encrypted delivery until explicit acknowledgement and replay
lost responses without reissuing. #94's one-time-response proposal is insufficient.
Historical User Manager totals require explicit current-period conversion.

Continue P2/P3 in the roadmap. Each scoped issue uses related checks, final full
gate, source review, exact-head/current-base PR CI, squash to develop, post-merge CI
and ledger update. Hardware remains Pending Hardware Verification until measured
on the actual target. Do not close the full platform from these bounded increments.


UI extension received 2026-11-13: deliver #97 shared sidebar/list/modal foundation
and record browser evidence in UI-UX-UNIFICATION.md. #98 delivered its foundation;
#99 uses those components for credential rotation. Continue broader audit and
commercial work with those same shared components. Do not claim
UX closure for unresolved directory/detail/analytics/backend dependencies.
