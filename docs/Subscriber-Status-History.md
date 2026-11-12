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
reads. Fresh local focused Go/SQLite and HTTP tests pass; the earlier SQLite
download restriction no longer applies here. Current-head full repository gates
and required GitHub CI remain separate delivery requirements.


## Synthetic scale measurement (2026-11-13)

`BenchmarkStatusHistoryScale` creates and asserts 100, 1,000, 10,000 and 100,000
synthetic accounts, one status audit per account and 100 extra entries on the
selected account. Each measured read calls the production Store.StatusHistory
and asserts a bounded 50-event page with a next cursor. Setup and password
hashing are excluded from per-operation timings. Authentication, HTTP, concurrent
load, network sessions and routers are not exercised.

Measured at source `02848d93c28473834f0d4d70fa7ab27f652258a3`, Go 1.27.3,
Linux/amd64, Intel Xeon E-2176M 2.70 GHz, benchmark concurrency setting 4:

| Synthetic subscribers | ns/op | allocated bytes/op | allocations/op |
|---|---:|---:|---:|
| 100 | 727262 | 21663 | 491 |
| 1000 | 666440 | 22093 | 543 |
| 10000 | 612502 | 22082 | 543 |
| 100000 | 776563 | 22096 | 543 |

The complete benchmark process, including all database setup, used 6.11s user
CPU and 2.41s system CPU in 7.25s wall time with peak RSS 29,912 KiB. These are
one-run local measurements, not latency percentiles, SLA, server memory usage or
proof of 100,000 concurrent subscribers. No RouterOS connection is opened.
Raw local output: `/tmp/mikrodash-80-scale.log`.

Reproduce independently of ordinary tests:

```sh
go test -c -o /tmp/mikrodash-history-bench.test ./internal/subscriber
/usr/bin/time -v /tmp/mikrodash-history-bench.test -test.run '^$' \
  -test.bench '^BenchmarkStatusHistoryScale$' -test.benchtime=250ms -test.benchmem
```

## Administrator view

Choose **Status history** on a subscriber row to open the history tab for that
account. The view clears old rows immediately on account selection and ignores
late responses from previous selections. It displays loading, empty and error
states; **Retry page** retains already loaded entries after an interrupted older
page. **Load older changes** passes the exact decimal cursor. **Refresh history**
starts again from the latest page. Repeated clicks during a pending request do
not issue another request. Source text is escaped and dates use the installation's
shared timezone formatter. Historical batch reasons remain the original evidence
string. No historical before-state or successful NAS application is inferred.

The view uses the same administrator API; it does not change authorization,
configuration or router state. It is an incremental status-audit view, not a
complete usage/device/general-audit screen. Local browser checks use synthetic
HTTP responses, the actual administrator page and vendored assets. They cover
account switching with delayed results, empty/loading states, a truncated older
page and retry, exact cursors, keyboard focus, refresh, and dark/light layouts at
375/1280px with axe accessibility checks. They are not live-router acceptance.

Review regressions also cover stale history rejection, initial-page failure and
retry, synthetic HTTP401/403 responses through the real fetch helper, and leaving
a tab while history loads. A reproduced inherited Inspect race was fixed:
Inspect responses and errors now respect newer Inspect/tab selections and leaving
the Subscribers page, so they cannot replace a newer history view or its notice.

Validated batch evidence displays its stored reason, before/after status and batch
ID as labelled text. Original audit record opens the unchanged source string.
Legacy, malformed or incomplete envelopes remain escaped literal text; no
transition is inferred. Stored status records never prove network application.
