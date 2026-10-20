# Subscriber Management verification record

Date: 2026-10-19 (Asia/Aden). Issue: [#38](https://github.com/so7ob/MikroDash/issues/38).
This is a **development preview, not acceptance-complete**. Passing the checks below does not
establish live HotSpot sign-in, CoA speed verification, production readiness or deployment.
See [architecture and remaining work](Subscriber-Management.md) and the
[lab guide](Subscriber-Lab-Guide.md).

## Source and isolation

Implementation took place in the visible `/root/So7ob/MikroDash` checkout, on
`feat/38-subscriber-management`, from `develop` at `18a5f7de`. The origin is
`so7ob/MikroDash`; neither the upstream reference nor `so7ob/web` was modified.
Unrelated `.vscode/` files and the running debugger binary were preserved and not staged.

Clean detached worktrees were used only for verification and deliberate negative mutations.
They contained the same committed source, without the operator's ignored runtime directory.
The visible checkout's baseline `go test ./...` fails `TestNoCommittedCredential` because
`.temp/geo/cities.json` exceeds that existing check's 8 MiB scan limit. The file and the check
were retained. A passing clean-checkout result does not mean that local warning disappeared.

## Commands and actual results

| Check | Evidence and limits |
| --- | --- |
| `sh tools/verify.sh` | Passed at `90ba843d`, and again after the late-byte correction at `5cdd4515`: gofmt, vet, 62 Go packages, govulncheck, six Go generators, seven generated-table checks, TypeScript, 141 frontend tests and 22 tooling tests. No skipped half in these runs. |
| Authentication/resource and multiple-limit follow-up | At `f704de47`, focused subscriber/server tests passed for shared credential concurrency, portal routes, simultaneous limits and cross-router budgets. Final branch CI is a separate check on the PR head. |
| `go test -race ./internal/subscriber ./internal/db -count=1` | Passed after the SQLite URI fix: subscriber 18.268 s, database 48.886 s. |
| `go test -race ./internal/server -count=1` | Passed at `c958eefa` in 297.350 s after initializing the pre-existing fleet fixture's hub. This is not a RouterOS timing measurement. |
| Portal race coverage | `go test -race ./internal/server -run '^(TestPortal\|TestShadowCannotIssueHotSpotTicket)' -count=1`: passed, 4.831 s. |
| Accounting fuzz | `go test ./internal/subscriber -run '^$' -fuzz '^FuzzAccountingMonotoneReplay$' -fuzztime=5s -parallel=2`: passed at `5cdd4515`, 91 executions in the final run. |
| Period fuzz | Same command with `FuzzPeriodContainsTimestamp`: passed at `5cdd4515`, 41,415 executions in the final run. Five seconds is a bounded probe, not exhaustive exploration. |
| Browser acceptance | Real Chromium, synthetic TLS server and SQLite: login, persisted speed choice with pending status, shadow connection refusal, logout, LTR, 375/1280, blue/green/purple, no page errors, and axe WCAG 2 A/AA + 2.1 AA checks passed. No real HotSpot was involved. |
| Patched RouterOS adapter | Existing adapter tests, including `TestNoReplyIsLostBeforeItsTagIsRegistered` and `TestTracingNeverLogsACredential`, remain in the passing Go suite. The vendored module itself has no standalone test files. |
| Live read-only protocol | Existing `cmd/conformance`: seven protocol cases passed against the configured router. This does not test subscriber RADIUS or internet access. |
| Existing data compatibility | `go run ./cmd/compat -data ./devdata`: all eleven reported checks passed, including the encryption envelope, bare user array, router credential decryption and audit reads. Password verification was not requested. |
| Builds | Frontend and host binary built. The additional ARM64 build did not complete during disk exhaustion; no ARM64 success is claimed. |

The browser harness is `tools/subscriber-browser.mjs`, invoked by
`go test -tags=subscriberbrowser ./internal/server -run '^TestSubscriberBrowserAcceptance$' -count=1 -v`.
The tested external tooling was Playwright 1.64.1 and @axe-core/playwright 4.14.0, with an
explicitly selected installed Chromium executable. Only the synthetic test context ignores
its self-signed certificate; production TLS verification is not disabled.

## Failures found and repaired

- A literal `#` in a SQLite filename was interpreted as URI syntax. The new fuzz seed exposed
  that defect. Creation and reopening now share escaped absolute URIs, with regression tests
  for `#`, `?`, `%`, spaces and Unicode. No data-directory contents were deleted to resolve it.
- One-second protocol-test deadlines were too short for race-instrumented scrypt under load.
  Correctness tests now retain a five-second bound; they do not assert a latency SLA.
- A pre-existing fleet-session test created a real asynchronous session manager with a nil hub.
  Delayed dial failure could panic under race instrumentation. Its fixture now initializes the
  hub, preserving the assertions and production code.
- A delayed reading with higher bytes and an older clock could invert a boundary interval.
  The correction now charges every touched period conservatively and never doubles lifetime
  counters. A midnight regression pins the case.
- The first axe run found muted text contrast failures. The portal now uses shared dark-theme
  attributes and an accessible scoped text token; the subsequent browser run passed.
- Additional checks and cross-compilation exhausted the host disk. Those runs failed and are
  not counted as passes. Rebuildable compilation caches were cleaned; the complete verification
  and both fuzz probes then passed again. Databases, volumes and operator runtime files were
  not deleted. No performance inference is drawn from these resource-contended runs.

## Negative mutation evidence

Four deliberate, non-equivalent mutations were applied one at a time in an isolated copy and
restored immediately. All four were killed by a test assertion, not by a compilation error:

| Mutation | Test that failed |
| --- | --- |
| Add cumulative seconds instead of taking the high-water mark | `TestDuplicateLateStopDoesNotCloseNewSession` |
| Ignore outstanding reserved balances | `TestAtomicSharedAdmission` |
| Delete usage buckets when changing speed | `TestTimeSurvivesSessionsAndSpeed` |
| Ignore the service disconnect grant | `TestSubscriberServiceGrantIsNarrow` |

The mutation run used `90ba843d`. Its logs do not claim exhaustive mutation coverage of the
later changes. No test, guard or check was removed to make a failed run green.

## Acceptance coverage

| Requested behavior | Implemented evidence / remaining boundary |
| --- | --- |
| One hour before first use; 20-minute reconnect; 109 + 51 seconds | Persistent-ledger tests prove 3600, 2400 and 3440 seconds remaining respectively. |
| Simultaneous time/download/upload/monthly total | Four independent limits, exhaustion reaching the background decision, denial of re-entry and daily renewal preserving monthly usage are tested. |
| Speed preserves balance and validity | Persistence and entitlement checks pass. Live speed application/readback is not implemented; the portal says `pending-reconnect`. |
| Duplicates, reordering, delayed Stop and reused session identity | Journal deduplication, independent counter high-water marks and lease-bound identity are tested. |
| Midnight, monthly periods and minute-25 hourly anchor | Calendar and conservative boundary-accounting tests pass. Exact byte distribution within an unsampled gap is not claimed. |
| One-second snapshot skew | Regression keeps known counters and balances; it does not convert the account to Unknown. |
| Multiple devices/routers share entitlement | Concurrent admission and distinct-router/network reservation tests pass. Multiple networks using the same NAS source address are still refused by runtime configuration. |
| No browsers open | Real loopback UDP test proves the background worker sends a guarded Disconnect independently of portal presence. Its NAS and API identity callback are synthetic. |
| Application restart and missing Stop | Actual SQLite close/reopen preserves counters, deduplication and reservations. Missing Stop remains fail-closed until final accounting; automated reconciliation is outstanding. |
| Exhausted/suspended account can view portal | Credential verification is separate from admission, with ownership and refusal tests. Live post-exhaustion walled-garden reachability is untested. |
| Unauthorized profile/account/device/ticket | Server ownership, CSRF, Origin, device-bound ticket replay, forged accounting, unknown fields and access to admin routes are tested. |
| Zero/unknown/unlimited, >4 GiB, Unicode | Quantity/string/BigInt, RADIUS gigawords and escaping tests pass. Import application and approved opening balances remain outstanding. |
| Existing MikroDash behavior | Full repository gates and data compatibility pass within the stated snapshots; live production behavior was not changed. |
| English/LTR and local design resources | New UI text is English, user data retains Unicode, existing fonts/tokens/logo are reused, three themes share one stylesheet. No i18n or RTL project was introduced. |

## Performance and artifacts

On the development host (linux/amd64, Xeon E-2176M),
`BenchmarkDurableDuplicateAccounting-4` measured 182,512 ns/op, 3,854 B/op and 108 allocs/op
for 6,302 operations. **This is duplicate-event transaction overhead**: it does not append a
new accounting row on every operation and is not a durable-write, flash-wear or router-load
benchmark. No measured production CPU, memory, API-channel or enforcement-latency budget is
claimed. Normal-condition five-second usage freshness is not achieved by the current
60-second RADIUS observation path.

Synthetic screenshots and local command logs are kept in the ignored workspace directory
`.screenshots/subscribers-38/`. Six PNGs are named
`subscriber-synthetic-{375,1280}-{blue,green,purple}.png`. They contain synthetic accounts,
not subscriber data. Raw live topology and User Manager output remain outside the repository.

The PR's Files changed view is the authoritative changed-file inventory. Runtime changes are
in `internal/subscriber`, `internal/server/subscriber*`, `internal/db`, `internal/guard`, the
session hold/RBAC/page integration, the subscriber frontend and build/type generators.
The two new commands are `cmd/subscribercheck` and `cmd/subscriberconfig`.

No merge, release, image publication, production deployment, router configuration write,
production Disconnect or authentication cutover is implied by this record.


## Administration without network runtime (Issue #47, 2026-10-20)

This independent fix starts from verified `develop` `dcf1e161`. The actual
running debugger has no subscriber configuration; no subscriber TLS/configuration
files were present in its data directory. No runtime was enabled or restarted.
A fresh read-only NAS inspection still reports 7.24.5 and C RADIUS accounting;
this does not constitute a subscriber hardware test.

The new HTTP/SQLite regression first reproduced the reported 503. After separating
administration from the optional runtime, focused subscriber server tests passed.
They exercise durable account creation, Unicode, immutable plan/assignment,
simultaneous time/download limits, zero-usage pending activation, status changes,
reconciliation and existing auth/Origin/storage guards with no running runtime.
Frontend tests passed: 145 tests, zero failures/skips.

Actual Chromium against protected HTTP handlers and SQLite passed (4.151 seconds),
including account creation while Disabled, persistent status after success,
375/1280 screenshots, axe and the existing reconciliation journey. The first new
browser sweep failed table-header contrast (2.76:1). Extending the existing
optional-page header token rule fixed the product; axe was not suppressed.
Synthetic evidence lives under `.screenshots/subscriber-offline-47/`, including
`subscriber-admin-offline-synthetic-{375,1280}.png`. These are not live accounts.

Full clean-checkout verification and exact PR/post-merge CI are delivery gates;
the final PR/issue receipts record their actual runs. This fix enables preparation
and inspection. It does not claim live HTTPS HotSpot sign-in, NAS capability proof,
authentication cutover or Internet connectivity.


The first full #47 sweep passed Go (62 packages), generators, 145 frontend tests
and 22 tooling tests, but failed TypeScript: `info` is not a valid `PillTone`.
The status now uses the existing `rx` tone. The failed sweep is retained as a
failure; a complete rerun is required on the corrected source. The final synthetic
browser run before that type correction passed in 4.728 seconds.


## Isolated HTTPS reverse proxy (Issue #49, 2026-10-20)

Implementation starts from verified `develop` `fe0db269` on its own issue branch.
No router write, deployment, active-session change or authentication cutover was
performed. The target's reported architecture is arm; prior static ARMv7 build
success does not prove target execution. Current RouterOS documentation describes
HTTP proxy upstreams; its 7.26 header description still needs a 7.24.5 target probe.

Actual commands (native Go 1.27.2, linux/amd64; compiler cache in RAM, SQLite files
in the ordinary temporary filesystem; synthetic accounts and scripted NAS reads):

- `go test ./cmd/subscriberconfig ./internal/subscriber ./internal/server -run '^(TestSeal|TestPortal|TestSubscriber|TestShadow)' -count=1`: exit 0
  (0.292 / 0.006 / 5.445 seconds). The sealing tool preserves explicit peer/transport
  configuration using the existing key and private-file rules.
- Focused proxy/HTTPS/RADIUS/startup tests: exit 0, 4.427 seconds. HTTP/TLS, cookies,
  SQLite and RADIUS packets run through real listeners. NAS host-table rows are
  scripted. The controlled edge appends its actual client after a forged address;
  the resulting ticket accepts only the verified device, permits an exact packet
  retry and rejects a new replay. Admin routes and socket commands stay isolated.
- `go test -race ./internal/server -run '^TestSubscriber(ProxyBoundary|HTTPSProxy|StartupServes)' -count=1`:
  exit 0, 8.807 seconds with native cgo/race instrumentation.
- `go test ./internal/server -run '^$' -fuzz '^FuzzSubscriberProxyNeverTrustsAnInjectedLeftClaim$' -fuzztime=5s -parallel=2`:
  exit 0, 14,939 executions. This is a short bounded property probe.
- `go test -tags=subscriberbrowser ./internal/server -run '^TestSubscriberBrowserAcceptance$' -count=1 -v`:
  exit 0, 14.373 seconds, native TLS and HTTPS-edge/private-HTTP paths. Actual Chromium
  covers login, speed choice/pending status, Shadow refusal, logout, three themes,
  English/LTR, 375/1280 and axe. Only the synthetic edge certificate is accepted by
  the test browser. Production native TLS verification was not disabled.

Failed probes are retained: the new config test first failed the former TLS-only
validation; a boundary case found a mapped-unspecified IP accepted as `0.0.0.0`;
the first integration cleanup called full teardown on a collector-free scripted
session and panicked. Config/normalization and fixture cleanup were corrected,
without weakening assertions or production teardown.

Screenshots are under `.screenshots/subscriber-proxy-49/` (native TLS) and its
`proxy/` child (HTTPS edge), with the existing clearly synthetic file names.
Full clean-checkout verification and exact PR/post-merge CI remain delivery gates;
final receipts belong to this issue/PR. No Internet session or RouterOS timing
claim is made from these synthetic integration runs.


Two non-equivalent mutations at implementation `b200018c` were killed by the
expected assertions in an isolated verification worktree: bypassing the verified
client at the ticket call site returned HTTP 400 instead of 200; removing the
exact proxy-peer condition admitted an untrusted peer and failed its 403 check.
Source was restored byte-for-byte before verification. A further login-limiter
call-site regression passed: changing a forged left claim cannot evade an exhausted
actual-client bound, and another verified client is not charged to the proxy.

Static `linux/arm`, `GOARM=7`, `CGO_ENABLED=0` build succeeded at `b200018c`; `file`
reports ARM EABI5, statically linked. It was not executed on the target. The first
full sweep reported Go failure without a retained filtered cause; it is not a
passing full run. The unchanged complete Go half then passed with trace/raw output,
including all packages, govulncheck and generators. No source/test failure cause
was proven, so none is invented. A complete sweep with raw Docker output retained
is required at the final head. Data files, volumes and operator caches were retained.


## Durable receipt continuation (Issue #46)

Current independent branch: `fix/46-durable-accounting-receipts`, based on verified
`develop` `baae4f3e`. See [receipt behavior](Subscriber-Accounting-Receipts.md).

- `go test ./internal/subscriber ./internal/db ./internal/server -count=1`: exit 0,
  13.775 / 13.219 / 61.063 seconds on the development host. SQLite uses ordinary
  filesystem temporary directories; only compiler caches use RAM.
- `npx --no-install tsc --noEmit` and `npm test --silent`: exit 0, 146 tests passed.
- `go test -race ./internal/subscriber -run 'TestReceipt|TestRealUDPIncomplete' -count=1`:
  exit 0, 9.065 seconds, native cgo/race instrumentation.
- `go test ./internal/subscriber -run '^$' -fuzz '^FuzzReceiptRangesAndRetryIdentity$' -fuzztime=5s -parallel=2`:
  exit 0, 37,000 executions. Valid uint64 wire values remain exact; unsupported
  signed-ledger sums/durations remain pending rather than wrapping. Financial
  identity ignores changed packet identifiers, delay and receipt time.
- Tagged `TestSubscriberAdminReconciliationBrowserAcceptance`: exit 0, 4.566 seconds,
  real protected TLS/HTTP/SQLite, original receipt fields, missing-versus-known
  counters, evidence-backed correction, account-response races, Unicode,
  English/LTR, 375/1280 and zero axe violations. Only NAS data is synthetic.

Failed probes remain recorded. The first recovery test did not compile because
it used the wrong operation signature; this is not counted as a behavioral
failure. After correction, the actual late-partial assertion failed. Parser
regressions also failed because malformed Start became zero and malformed values
shared an identity. The corrected cases pass. No test or assertion was removed.

The new credential-whitelist and unknown-NAS tests are supplemental; final full
verification, mutation checks, current-head CI and post-merge CI remain delivery
gates. Screenshots remain private under `.screenshots/subscriber-receipts-46/`.
No target-router traffic, reboot, power-loss durability or latency acceptance is
claimed. A successful commit is not itself an exact-head verification result.


At implementation `296a7bb5`, `sh tools/verify.sh` passed in an isolated checkout:
62 Go packages, gofmt, vet, live govulncheck, six Go generators, TypeScript, seven
JavaScript generators, 146 frontend tests and 22 tooling tests. Raw Docker output
was retained; no check was skipped. Compiler caches use RAM; SQLite stays on the
ordinary filesystem. The disk briefly approached capacity during concurrent
compilation; both commands completed successfully and released their temporary
files. No operator data, image, volume or unrelated cache was deleted.

The extended administrative browser passed in 7.316 s across default dark,
default light and Nord at 375/1280 with zero axe violations. The built portal
browser passed in 13.551 s across native TLS and the explicitly trusted HTTPS
proxy, each with clean and pending-receipt accounts. Pending snapshots display
20 confirmed seconds and 00:59:40 remaining, preserve known values and explain
blocked admission. Screenshots are clearly synthetic; no real NAS was used.

Two non-equivalent mutations were killed by compiling behavioral assertions:
acknowledging a failed receipt/debit and allowing API Start/final Stop. Their
source files were restored byte-for-byte. The initial mutation runner searched
for the wrong diagnostic phrase; its retained output already contained the
correct failing assertion, which was then validated explicitly. This was a
runner diagnostic error, not an additional product regression or a passing test.

The source and tested behavior are unchanged by this evidence update. Final
revision verification and exact-head/post-merge CI remain required before delivery.
