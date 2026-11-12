# Blockers and boundaries

Snapshot: 2026-11-13 (Asia/Aden). Baseline `ba39515f003484211afe3dfef783ade6dc39f813` on `develop`; `main` = `ca2b1d5284d2da98967a5bc482282ee85d02bc8e`. Issue #95, parent #38. Source inspection is not an executed test or physical-device acceptance.

| ID | Class | Actual condition | Affected work / safe independent action |
|---|---|---|---|
| HW-C | Hardware/external authority | #76 L009/C admission/CoA/counter/outage acceptance not performed here | Keep hardware claims pending; continue software increments |
| PR-BASE | Integration | #77-#80 fork from de7dd435; current develop ba39515f; #81 stacked on #80 | Preserve branches, resolve against current develop, rerun exact-head/current-base checks |
| VOUCHER-REF | Existing work unavailable | #94 closed not_planned; no local/remote feat/94-voucher-batches or voucher commits found in accessible refs/reflogs | Document absence; do not claim recovery of unknown work; new scoped implementation |
| COMMERCIAL-POLICY | Product contract | Reset/rollover/proration/import opening balances lack reviewed explicit rules | Implement reviewable explicit policy contract before granting credit; continue unrelated work |

Prior notes mentioned a blocked SQLite download; that is historical and must be checked before reuse. Current test results will establish whether it remains a blocker. No network or dependency restrictions will be bypassed. No real payment gateway or production cutover authorized.

Local gate isolation: ignored `.temp/activity-82-upgrade/check.go` is not gofmt-clean, so running the unmodified full gate in the operator checkout stops before Go tests. Use a clean detached worktree at the exact candidate SHA. This is a verification-environment constraint, not justification to edit scratch files or weaken gofmt. #77 clean-worktree gate passed.

Resolved policy question: on 2026-11-13 the owner chose new commercial periods without rollover, retaining prior subscriptions and usage. This decision removes the commercial rollover-policy blocker. It does not authorize resetting accounting buckets in place or waiving idempotency/reconciliation.
