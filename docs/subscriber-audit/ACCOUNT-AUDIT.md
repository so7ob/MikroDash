# Administrative account audit (#101)

Continue P0-AUDIT after #99, keeping the delivered status-only history and all
A-N/P0-P3 and #97 requirements. This surface reads existing committed evidence;
it does not change account, credential, subscription, usage or network state.

Scope: account creation, name/status/batch/credential changes and historical
subscription assignment/speed-choice requests belonging to the selected account.
Ownership is persisted account/subscription identity, never inferred from current
plan, username, router or reason text. Shared plan publication, NAS/session and
accounting events are outside this administrative chronology. Its rows are not
proof that RouterOS applied an action. Future commercial receipts will add their
own explicit event contract.

Use index-only migration 47, exact exclusive audit-ID cursors (50 plus one
lookahead), server category and inclusive UTC date filters. Preserve tied/sparse
IDs and original safe evidence; never reconstruct historical before-values from
current mutable account state. Credential details permit only the existing
secret-free reason/beforeRevision/afterRevision envelope; malformed or unknown
credential payloads expose no raw evidence. Oversized other evidence is bounded
and explicitly labelled truncated.

The protected GET API uses the existing global modern subscriber-administration
authority and works while portal/RADIUS runtime is disabled. The row Audit action
opens the existing managed modal, with shared cards/fields/table and readonly
filters that require no unsaved-edit confirmation. Loading/error/empty/older-page
and retry states must retain a truthful account/filter scope, fence late success
and rejection, preserve exact IDs and restore keyboard focus. No new sidebar,
component library, decorative metrics or browser prompt is added.

Executed verification, representative images and delivery SHAs/CI are recorded
with the completed implementation and on Issue #101. No deployment, activation,
router write or hardware acceptance is part of this issue.


## Executed checks and scale evidence

Focused store/migration/API tests pass: committed writer chronology, old owned
subscriptions, colliding IDs across entity domains, exact >2^53 audit IDs,
0/1/49/50/51/100/101 row boundaries, intervening writes, UTC date/category filters,
query-only reads, retained evidence and no phantom events after failed writes.
Actual SQLite plans use subscriber_audit_target for both event domains and reuse
the existing subscriber_subscription_account ownership index. Each ordered branch
is clipped to 51 candidates before the final 50-row merge. No duplicate ownership
index or audit-writer/credential/admission behavior is introduced.

Credential evidence rejects unknown, duplicate and case-variant fields, malformed
or incomplete revisions and trailing JSON. It has no raw fallback. Other evidence
is bounded to 8,192 Unicode characters and labelled when truncated. Valid evidence
retains its original text; parsing is presentation only.

Eleven controller/rendering tests pass, including late success/rejection after
account/filter/close changes, malformed and mismatched page refusal, exact cursors,
interrupted older-page retry, escaped Unicode and truthful missing before-state.
The frontend census is 118 test files; final full-suite counts are recorded on
#101 with the tested delivery head.
The protected HTTP/SQLite browser covers real 401/403/400, 59 original rows across
two pages, retained reading position and exact-ID focus, current/historical
subscriptions, readonly Escape and filter/account races. It checks dark/light/Nord
at 375/768/1280/1920, shared field geometry (including a deliberately broken input)
and axe. Final head/results are recorded on #101 after delivery.

### Synthetic indexed-reader measurement

Command: compile internal/subscriber tests with go test -c, then run
/usr/bin/time -v /tmp/mikrodash-101-subscriber-bench-final.test -test.run '^$'
-test.bench '^BenchmarkAccountAuditUnrelatedScale$' -test.benchtime=250ms
-test.count=1. Go 1.27.3, linux/amd64, Linux 6.1.0-42-amd64, Intel Xeon E-2176M
2.70GHz, four visible CPUs. Raw log: /tmp/mikrodash-101-scale-final.log.

| Unrelated accounts/subscriptions | Selected 50-event read | Allocated bytes/op | Allocations/op |
|---|---|---|---|
| 100 | 2.602231 ms | 42,574 | 794 |
| 1,000 | 2.378689 ms | 42,055 | 792 |
| 10,000 | 3.356548 ms | 41,954 | 792 |
| 100,000 | 4.891236 ms | 41,942 | 792 |

Each population also has one selected account/subscription, 60 selected events
and two unrelated events per unrelated account. Actual counts and returned rows
are asserted. The whole benchmark process, including all seeding and repeated
populations, took 24.52s wall, 13.48s user/4.90s system CPU and peak RSS 27,688KiB.
This is one warm single-connection SQLite reader on a shared local host; browser
validation ran concurrently, so CPU was not isolated. The earlier TEXT-prefix
measurement is retained in /tmp/mikrodash-101-scale.log; the table above measures
the final NUL-preserving reader. It does not measure HTTP/auth,
cold-disk latency, service RSS, 100k sessions, a large per-account subscription
history, concurrent writers or RouterOS resources. It opens no router connection.

Retained corrections: the initial readonly fixture tried to run the existing
immediate-transaction snapshot while query_only was enabled. Audit itself succeeds
with writes refused; snapshots are compared after the fixture restores transaction
capability. The query-plan test now asserts the actual pre-existing ownership
index. Browser review caught uppercase styling on account identifiers and excess
mobile controls/cards. Identifiers now keep their original case; compact summary,
collapsed filters, shared responsive utilities and section cards make phone audit
evidence readable. No static or functional check was removed or weakened.

A final data-integrity regression proves embedded NULs and four-byte Unicode
boundaries retain exact safe evidence. The reader uses a bounded blob prefix;
control characters in original UI evidence are labelled JSON escapes. The initial
failure is retained in /tmp/mikrodash-101-nul-initial.log.
