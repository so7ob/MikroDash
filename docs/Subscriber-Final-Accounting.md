# Final accounting after a missing Stop

A complete shared API read can prove that a session is absent after every
possibly issued local delegation has expired. This closes its concurrency seat;
it does not prove its final time or byte counters. Local byte-cap overruns were
measured in the CHR lab, so a delegated byte amount cannot silently be treated as
an exact final debit or an unconditional upper bound.

## Grant and review contract

The existing SQLite ledger, RADIUS service and guarded background worker remain
authoritative. An absent session's service reconciliation record remains pending
until a trusted complete Stop or reasoned final reading records a final Stop
event. New admission and delegation renewal are refused account-wide during
that interval. Previously queued operations also encounter the current gate
before sending. Existing devices retain their bounded local delegations.

Known usage, reserved exposure, immutable plan/subscription identity and expiry
remain intact. Portal authentication/account access stays available. No estimated
hold is rewritten as measured usage. The snapshot reports `pendingAccounting`,
`reconciling` and `accounting-reconciliation-required`, using the existing portal
message and protected Reconciliation workflow.

A session adds one pending issue only if no pending receipt already represents
its finality. Ordinary byte-distribution holds after a trusted final Stop remain
independent: their observed total is known, while its period allocation is not.
An unlimited subscription can have zero quota holds and still require final
accounting; its session appears in the review queue with zero unresolved limits.

A late trusted Stop resolves only its own lease, preserves high-water counters
and cannot close a subsequent lease. A reasoned administrative final reading
uses the existing permission/Origin checks, explicit evidence, non-regressing
counters, atomic audit and idempotence. Audit failure leaves the gate and all
balances unchanged. Another account's admission is unaffected.

## Migration and rollback

Migration 42 adds only three indexes. Partial indexes cover final Stop events
and service absence records; rapid Interim rows do not gain extra index entries.
The subscription/account index bounds account history lookup. Existing records
supply the finality evidence without destructive backfill or rewritten balances.
Query-plan tests verify indexed searches rather than full journal scans.

Fresh and migrated databases agree. Older builds' minimum-schema reader can
read these additive indexes, but the previous code does not enforce this gate.
If rolling back code, keep network admission disabled until its uncertainty is
settled and routing is explicitly reviewed. Do not restore an old database over
new accounting or remove history to release credit.

## Evidence and limits

Real isolated CHR 7.24.5 testing under #54 first found the defect. During the
normal runtime's deliberate outage, the device had 44 seconds left and a complete
API read saw no active session after 44615.115 ms, including polling latency.
Stop did not recover. Restart closed the seat and retained three unresolved
holds, yet the old snapshot allowed new admission. No operator NAS was changed.

The new regressions first failed behaviorally on verified develop `7d6f8121`:
the reopened real UDP receiver sent Access-Accept, and another device could
prepare renewal. The correction gates the actual shared transaction paths.
Focused tests cover limited/unlimited plans, zero-hold review, database reopen,
UDP final Stop/retransmission, late Stop after a new grant, pending-receipt count
deduplication, independent period allocations, new/queued renewal, account
isolation, review rollback and index-only migration with identical snapshots.

The initial reconnect fixture crossed its configured daily reset, and two
boundary fixtures had a different device/anchor than intended. Those failed
runs were retained; the fixtures now isolate the intended behaviors without
weakening their assertions. Full final-source, mutation/race and current-head
pre/post-merge CI remain separate delivery gates.

Go 1.27 makes `testing.T.TempDir` follow GOTMPDIR. Compile a test executable in
RAM if needed, then run it with both GOTMPDIR and TMPDIR on ordinary storage:

```sh
go test -race -c -o /path/to/ram/subscriber.test ./internal/subscriber
GOTMPDIR=/tmp TMPDIR=/tmp /path/to/ram/subscriber.test -test.count=1 -test.v
```

Only the compile command may use a RAM GOTMPDIR. Earlier local runs with RAM
GOTMPDIR are behavioral checks; descriptions claiming ordinary SQLite test
storage for those commands were incorrect. GitHub CI and the real runtime's
separate disk ledger are different evidence. No L009/C, automatic landing,
complete module or production-readiness claim follows from this focused fix.
