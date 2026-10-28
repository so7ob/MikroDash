# Subscriber status history

The administrator-only `GET /api/subscribers/status-history?account=<id>` route
reads existing account status audit entries, including legacy single-account
changes and reviewed batch changes. It requires the same modern session and
`system:subscribers` permission as other subscriber administration routes and
sets `Cache-Control: no-store`. It works while RADIUS is disabled.

Responses contain `accountId`, `events` and, when another page exists,
`nextBefore`. Events contain decimal-string `id`, `actor`, `action`, original
`reason` and Unix-seconds `createdAt`. Batch reasons retain the existing JSON
string envelope (batch ID, reason, before, after); legacy reasons remain plain
text. No missing historical before-state is inferred. This records administrative
intent, not proof of NAS application or session disconnection.

Each page contains at most 50 events ordered by descending audit ID. Pass the
returned string as `before` for the next page; do not convert it to a JavaScript
number. The cursor is exclusive and must be a canonical positive signed-64-bit
decimal string. Omit it for the first page. Newer inserts do not displace older
pages; restarting without a cursor sees newer entries. This is not a retained
snapshot or export. Empty history is `events: []` with no next cursor. Unknown
accounts and malformed cursors are rejected. Cursors never change account scope.

Only `account.active`, `account.suspended`, and `account.status.batch` actions are
returned, using the existing account status index. No migration or new writes are
introduced. Unrelated audit actions, credentials, balances and other accounts are
excluded. This API increment addresses part of #38's administration gap; a browser
history view, general audit/usage screens and full device history remain pending.
Actual L009/C acceptance under #76 remains untested.

Regression tests use isolated SQLite fixtures for account/action isolation,
50+3 pagination, inserts between pages, repeated cursors, IDs above 2^53, batch
reason preservation, empty/invalid requests, authorization and disabled-runtime
reads. Local full-package execution remains blocked by the pinned SQLite archive
restriction; final-head GitHub CI is required before delivery.
