# Verified progress and delivery ledger

Date: 2026-11-13 (Asia/Aden). Initial develop ba39515f003484211afe3dfef783ade6dc39f813;
main ca2b1d5284d2da98967a5bc482282ee85d02bc8e. Main was inspected and remains the
release line; no promotion, release, deployment, production cutover or router write
was performed. Audit Issue [#95](https://github.com/so7ob/MikroDash/issues/95), parent
[#38](https://github.com/so7ob/MikroDash/issues/38).

| Increment | Tested head | Squash merge into develop | PR CI | Post-merge CI | State |
|---|---|---|---|---|---|
| #77 quantities and full command bodies | bdc31ae39378ea2eceaa6ec777a1714a96956339 | 149123b76a882b6811cf6052b5c34d1d7461afb3 | [43307893327](https://github.com/so7ob/MikroDash/actions/runs/43307893327) PASS | [43308821709](https://github.com/so7ob/MikroDash/actions/runs/43308821709) PASS | Delivered |
| #78 structured plan draft limits | 293bcb6a187b45f8dbb6fee3975422ac81d40ea8 | b29ee7d6c4220f7b053d00689043ce9b7824ee9e | [43309860407](https://github.com/so7ob/MikroDash/actions/runs/43309860407) PASS | [43310757862](https://github.com/so7ob/MikroDash/actions/runs/43310757862) PASS | Delivered |
| #80 paged status API and scale benchmark | 6e7fcfcf6d4d110487c56b4bc14bf4f6a1bd2dca | ca6d5e950f17e8b1987fc77df41b80e63f4685ec | [43311070342](https://github.com/so7ob/MikroDash/actions/runs/43311070342) PASS | [43312107877](https://github.com/so7ob/MikroDash/actions/runs/43312107877) PASS | Delivered |
| #81 readable status history and real SQLite browser | e738a3f8c148ff6937dd697dd59b1925fcac00d3 | 4f1828c7da745b221f6ed36b40e5f080c4555943 | [43313964236](https://github.com/so7ob/MikroDash/actions/runs/43313964236) PASS | [43314920405](https://github.com/so7ob/MikroDash/actions/runs/43314920405) PASS | Delivered |
| #79 validated read-only source preview | 952f79a8a85ff91489a03a8240fd57f363bb8a49 | 4a000c0ce046a1f20a018961d1e6a47fee42a71d | [43314958884](https://github.com/so7ob/MikroDash/actions/runs/43314958884) PASS | [43316199013](https://github.com/so7ob/MikroDash/actions/runs/43316199013) PASS | Delivered |

All five original feature branches are retained as requested in their existing
work. Merge conflicts were resolved with current develop without force-pushing.
Review was a source self-review; no independent approval is claimed. No blocking
review conversations existed at delivery checks.

New work includes protected HTTP/SQLite plan-version/FUP regression, synthetic
100/1k/10k/100k history benchmarks, genuine HTTP/SQLite history browser assertions,
readable batch history with original evidence, and the eleven requested audit
files. FEATURE-MATRIX decomposes 229 named acceptance checkpoints; their size and
scope differ, so they are not an overall completion percentage.

Owner policy confirmed: renewal and plan changes begin a fresh period without
rollover, retaining historical subscriptions and usage. This resolves the policy
question; it does not implement receipts, concurrency or idempotent fulfillment.

#38 remains open. #76 remains open and Pending Hardware Verification. #94 is closed
not_planned; no feat/94-voucher-batches ref, matching local branch, or recoverable
voucher batch implementation was found in accessible refs/reflogs/PRs. No unknown
lost commits are claimed recovered. A new voucher issue must cover recoverable
secret delivery and chunk-safe atomic publication before implementation.

Existing .vscode/, two cmd/mikrodash/__debug_bin* files, ignored scratch files,
operator data, owner debugger and unrelated worktrees remain preserved. Own clean
verification worktrees isolate the ignored scratch formatting issue; they do not
change the tested source or weaken gates. Issue #95 records the audit PR's own
final SHA and CI, avoiding a self-referencing source commit.


## UI/navigation foundation (#97)

Added an independent expandable Subscriber Management sidebar category and six
implemented pages; shared list-first cards/tables, guarded form/review dialogs,
real snapshot detail and time simulation, exact units and genuine scoped counts.
The protected HTTP/SQLite browser passed (286.528s), plan/history browser matrices
passed, TypeScript and 168 frontend tests passed. Representative before/after
images and per-page partial status are in UI-UX-UNIFICATION.md. This is an additive
foundation: #38, #76 and the full #97 design acceptance remain open. Continue
P0-CREDENTIAL and the retained commercial/policy/directory roadmap. The final
source SHA and local/PR/post-merge gate evidence are recorded on #97 after delivery.


## Administrator credential rotation (#99)

Built on verified #98/develop ecf191da, using the shared subscriber row actions
and managed modal. Schema 46 adds durable account/ticket credential revisions
without modifying existing account/hash/Unicode rows. A reviewed reset commits
scrypt replacement, old voucher/ticket revocation and secret-free audit atomically.
HTTP/WS authorization and authenticated RADIUS reservation fence stale epochs;
new logins survive old-session eviction. Existing subscription balances, usage,
leases and reservation exposure are preserved. Existing network connections are
not forcibly disconnected. Username editing/new vouchers/self-service recovery
remain separate work.

Focused store/migration/server race and failure checks pass, as do 169 web tests.
The credential browser passed 12 dark/light/Nord cases at all four widths with
zero axe findings, retained draft/conflict/uncertain-commit checks and real
HTTP/SQLite authority. CREDENTIAL-ROTATION.md records the contract and UI evidence.
The full stable gate, exact tested SHA, PR, squash SHA and pre/post-merge CI are
recorded on #99 after final delivery. Keep #38/#76/#97 open. Next P0: broader
account audit, followed by commercial idempotency; the A-N/P0-P3 roadmap remains.
