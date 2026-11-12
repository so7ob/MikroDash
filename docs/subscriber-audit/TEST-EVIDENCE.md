# Test evidence and practical limits

Date: 2026-11-13. Exact reviewed heads, merge SHAs and remote runs are in PROGRESS.
All named local results below were executed. Existing test files alone are never
a pass. Physical L009/C, PPPoE, real payments and production deployment were not
exercised. CLI builds and synthetic readers do not contact a real router.

| Revision / scope | Actual command/evidence | Result |
|---|---|---|
| ba39515f baseline | go test ./internal/subscriber ./internal/server ./internal/guard | PASS: 69.748s / 128.914s / 0.225s |
| #77 bdc31ae3 | Complete sh tools/verify.sh in clean identical-SHA worktree; production frontend build; diff check | PASS: format/vet/64 Go packages/govulncheck/all Go generators, tsc/7 table generators, 156 web, 32 tooling |
| #77 bdc31ae3 | Tagged TestSubscriberBrowserAcceptance and HandoffBrowser | PASS 119.556s: real TLS/HTTP/SQLite, Arabic RTL account/voucher/QR/WS, Shadow refusal, 375/1280/keyboard/axe; NAS synthetic |
| #78 293bcb6a | Complete clean-worktree verify and production frontend build | PASS: 64 Go packages/govulncheck/generators/tsc, 159 web, 32 tooling |
| #78 application source | TestSubscriberPlanEditorPersistsConcurrentLimitsAndImmutableAssignment | PASS: real protected HTTP/SQLite, >2^53 exact quantities, sparse limit IDs, time zone/FUP, immutable old subscription, refused overwrite, no runtime activation |
| #78 editor / unchanged #81 editor | tools/subscriber-plan-browser.mjs | PASS: add/edit/cancel/remove; invalid/interrupted/refused/repeated submissions; 6 dark/light/Nord 375/1280 axe cases, zero findings; synthetic API |
| #80 6e7fcfcf | Complete clean-worktree verify; focused StatusHistory tests | PASS: 64 Go packages/govulncheck/generators/tsc, 159 web, 32 tooling; exact IDs, account/action isolation, query-only SQL, pages 0/1/49/50/51/100/101, auth/offline/storage failures |
| #80 source 02848d93 | BenchmarkStatusHistoryScale, 250ms per population, compiled native binary timed with /usr/bin/time -v | PASS: asserted 100/1k/10k/100k synthetic accounts; 50-event reads 0.612502-0.776563ms/op, 21,663-22,096 allocated B/op; whole process 7.25s wall, 6.11s user/2.41s system CPU, peak RSS 29,912KiB |
| #81 e738a3f8 | Complete clean-worktree verify and diff check | PASS: 64 Go packages/govulncheck/generators/tsc, 166 web, 32 tooling; production app source build passed |
| #81 e738a3f8 | Tagged TestSubscriberAdminReconciliationBrowserAcceptance | PASS 101.811s: actual protected HTTP/SQLite, committed batch history, parsed/original evidence, native Enter/focus, account isolation, retained name/bulk/reconciliation flows; six history appearance/viewport axe cases zero findings; screenshots inspected |
| #81 unchanged controller/editor source | tools/subscriber-history-browser.mjs | PASS: four dark/light 375/1280 axe cases, exact cursors, delayed results/rejections, interrupted retry, synthetic HTTP 401/403 and navigation |
| #79 952f79a8 | Complete clean-worktree sh tools/verify.sh and diff check | PASS: 64 Go packages/format/vet/govulncheck/generators/tsc, 166 web/32 tooling; PR CI 43314958884 and post-merge 43316199013 PASS |
| #79 implementation unchanged at final candidate | Focused Preview/Import tests; go vet ./internal/subscriber ./cmd/subscribercheck; CLI build | PASS; no router contacted |
| #79 implementation at isolated e5fa2f53 | go test ./internal/subscriber -run '^$' -fuzz '^FuzzImportPreviewIgnoresUnrequestedFields$' -fuzztime=5s -parallel=2 | PASS 16,597 executions in 5.066s; complete final-head repository gate also passes |

The benchmark measures one selected store page, not HTTP authentication, latency
percentiles, a running service's RSS, concurrent sessions, router resources or
whole-platform 100k readiness. Exact environment, per-population numbers and
reproduction are in ../Subscriber-Status-History.md. It opens zero RouterOS
connections. Synthetic network/browser success does not satisfy #76 hardware.

Retained failures and corrections:

- Visible-checkout full gate stopped at ignored .temp/activity-82-upgrade/check.go
  formatting before Go tests. The file was preserved; the same source SHA passed
  the unmodified gate in a clean worktree. No check was skipped to claim success.
- #78 CI 43309347585 failed a stale 115/116 frontend-file census after automatic
  merging. Both documents were corrected; claim-check and all new-head gates pass.
- #81 genuine browser initially expected decoded brackets inside retained JSON
  evidence. It now asserts parsed reason plus exact source text. A later native
  disclosure click correctly changed focus; final tests assert tab focus before
  the action and summary focus after keyboard Enter. Both failed logs remain.
- The first #79 fuzz build lost a file during an agent branch switch. It is a
  setup failure. Re-running in an isolated fixed-SHA worktree passes; source was
  unchanged. No failed, stale, skipped or pending run is counted as final proof.

Local artifacts: /tmp/mikrodash-77-clean-verify.log, -77-browser.log;
-78-final-verify.log, -78-editor-evidence/; -80-verify.log, -80-scale.log;
-81-final-verify.log, -81-final-browser.log, -81-final-browser-evidence/;
-79-isolated-fuzz.log and -79-verify.log (passed).
Remote PR and post-merge CI links are authoritative in PROGRESS. The audit PR's
own exact head/full gate/CI is recorded in Issue #95 after finalization.
