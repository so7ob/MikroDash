# Resume checkpoint and next implementation

Work in /root/So7ob/MikroDash. Inspect status/remote/worktrees and fetch current
develop before changing anything; preserve .vscode/, debug binaries, operator
scratch/data and existing feature/checkpoint branches. Read CONTRIBUTING.md,
PROGRESS and TEST-EVIDENCE. Issue #95 records this audit's own delivery SHA/CI.
Do not repeat already passed gates without a new change or unresolved failure.

Current integration: #77, #78, #80, #81 and #79 are delivered, with successful
full local and PR/post-merge CI. Issue #95 tracks the audit PR and its own exact
head/CI. Keep #38/#76 open; no release or runtime activation is implied.

Next independent P0 is audited credential rotation. Search issues first, create
or reuse a scoped issue and numbered branch from then-current develop. Deliver a
reviewed administrator UI/API with modern authority, Origin/CSRF, explicit account
and reason, stale credential-version fencing and scrypt using existing primitives.
Commit new credential/version, audit and outstanding-ticket revocation atomically.
Portal HTTP and WebSocket sessions must reject revoked epochs, including an
in-flight login/reset race; RADIUS admission must not accept stale authentication
after reset. Preserve commercial entitlement, usage and existing network session
history. No secret readback/logging. Test old/new credentials, concurrent resets,
login/admission races, audit/storage rollback, sessions/tickets, unauthorized calls,
input retention/uncertain response and actual protected HTTP/SQLite browser UX.
Define the credential epoch contract before editing network admission code.

Follow with broader account audit and P0 commercial operation idempotency. The
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
and record browser evidence in UI-UX-UNIFICATION.md, then continue the unchanged
P0 credential epoch/rotation work above using those same components. Do not claim
UX closure for unresolved directory/detail/analytics/backend dependencies.
