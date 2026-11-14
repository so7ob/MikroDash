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


## #97 UI/navigation foundation, 2026-11-13

The final protected HTTP/SQLite browser passed in 286.528s; 12 plan and 8 history
synthetic appearance/viewport cases passed with no axe findings. The six module
pages were audited/captured at 375/768/1280/1920 in dark/light, with dialog Nord
cases retained. Native required fields, failed/stale/repeated saves, original
Unicode, batch atomicity, read-only snapshots, evidence conservation, internal
modal scrolling, focus, active/deep links/back/forward/reload/short mobile drawer
and a labelled 5,001-row synthetic DOM case are covered. Frontend typecheck and
168 tests pass; focused RBAC/server/static contracts pass. Raw local artifacts:
`/tmp/mikrodash-97-final-sqlite-browser.log`, `/tmp/mikrodash-97-final-sqlite-evidence`,
`/tmp/mikrodash-97-plan-evidence`, `/tmp/mikrodash-97-history-evidence`.
Committed representative images and exact effective-source hashes are in
UI-UX-UNIFICATION.md and ui-evidence/manifest.json. Baseline source is 404622b3.

Retained corrections: initial static gates caught catalogue/field/help/unit-ID
wiring omissions; initial browser passes caught real muted secondary-action,
field-label/table-header and light-warning-pill contrast. These were corrected
in shared components/tokens. A geometry mutation was moved from the inert page
to the visible review toggle. Navigation tests now focus the actual sidebar
item and history targets its independent page. These are corrected failures,
not tests removed or retries of unchanged failures. Existing operator scratch
is preserved; the complete final gate runs in an isolated clean worktree.
Final head/full gate/PR CI/merge/post-merge CI are recorded in Issue #97. No
hardware/production activation is implied, and full platform/design dependencies
remain open in the feature matrix.

The first complete container gate also caught the exact no-settings-toggle census (14 -> 19 after adding five child pages) and a template-parser ambiguity from injecting required text. The census was updated for the new real pages and the existing toggleHtml inputAttrs argument is now used. Both targeted gates pass. Self-review replaced Number coercion in copied limit IDs with BigInt; the >2^53 non-reuse regression passes. The first full-gate failure is retained at /tmp/mikrodash-97-full-verify-initial.log.


## #99 administrator credential reset

Store/migration/server focused checks passed on the changed implementation.
`npm test` passed all 169 frontend tests without skips. The new tagged
`TestSubscriberCredentialBrowserAcceptance` passed in 108.847s after the mobile
name wrapping correction: 12 dark/light/Nord × 375/768/1280/1920 geometry/axe
cases (zero findings), real protected HTTP/SQLite conflicts/validation and
committed lost-response recovery, session revocation, keyboard/focus and safe
Unicode. Audit is independently inspected through a read-only SQLite connection.
An earlier passing browser (89.857s) exposed long-name clipping during image
inspection; the corrected images are retained. Later safe 503 classification
and actual audit-trigger failure are verified by focused server tests.

Commands, final exact-head full gate, optional browser regression, tested SHA,
PR CI and post-merge CI are recorded on Issue #99 after delivery. Artifacts use
`/tmp/mikrodash-99-*`. CREDENTIAL-ROTATION.md defines the durable epoch and
admission boundary; UI-UX-UNIFICATION.md links committed synthetic screenshots.
No actual NAS, router disconnect, deployment or production migration is claimed.

The first full #99 gate caught an explicit form-ID binding omission in
TestTemplateIDsAreBound. The same submitted form is now bound through the shared
DOM helper. The static check remains intact. Initial failure is retained in
/tmp/mikrodash-99-full-verify.log; final evidence uses the corrected head.


## #101 administrative account and subscription audit, 2026-11-14

Focused store/migration/protected API tests pass (7.848s/0.477s/0.845s), with
query-only reads, exact >2^53 keyset boundaries, persisted ownership despite
cross-domain ID collisions, UTC/category filters, rollback/no phantom history,
strict safe credential evidence and storage/authority failures. Branch-limited
query changes also pass the same tests. TypeScript and the earlier full frontend run (179 tests) passed;
eleven targeted controller/render tests, including the later control-character
regression, cover account/filter/close response races,
malformed/partial pages, retries, evidence and Unicode.

Actual protected HTTP/SQLite browser initially caught section-heading uppercase
on identifiers. Corrected browser passed 74.362s; a further layout round passed
81.550s, adding readable mobile section cards and collapsed filters; reading
position coverage passed 86.326s. These are real synthetic-history browser runs,
not a claim about hardware. Final selected head/full gate/browser/PR/develop
runs are recorded on #101. Raw artifacts: /tmp/mikrodash-101-*.

BenchmarkAccountAuditUnrelatedScale passed asserted 100/1k/10k/100k unrelated
account/subscription populations: actual selected 50-event SQLite reads
2.378689–4.891236ms/op, 41,942–42,574 bytes/op, 792–794 allocations/op. Whole-process
peak RSS 27,688KiB includes seeding; it is not runtime/service RSS. Exact commands,
environment, results and practical limits are in ACCOUNT-AUDIT.md.

Final pre-commit visual polish passed 86.029s with exact dates, wrapped actors,
phone cards and retained reading position. Representative frames and visual-source
hashes are committed in audit-evidence/manifest.json.

The final NUL-preserving reader passes its focused integrity tests and the
added control-character rendering regression. Final benchmark source/log is
/tmp/mikrodash-101-scale-final.log; shared-host CPU was not isolated. The previous
TEXT-prefix benchmark remains historical evidence, not the final performance claim.

The final corrected implementation browser passed 112.748s with twelve visual/axe
cases and independently verified unchanged account/credential/usage/audit state.
The final source hashes and refreshed representative images are in audit-evidence/.

The first full gate at 352ac7c1 passed 180 frontend and 32 tooling tests but
correctly rejected an unread phone-card data attribute. The marker was removed;
the browser now uses native article semantics. The check is unchanged. Initial
failure: /tmp/mikrodash-101-full-verify.log. Corrected-head full proof is on #101.
