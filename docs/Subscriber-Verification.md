# Subscriber Management verification record

> Storage correction (2026-10-20): Go 1.27 makes `testing.T.TempDir` follow
> GOTMPDIR. Historical local claims of ordinary SQLite temporary storage are
> not disk-persistence evidence when the command put GOTMPDIR in RAM. Behavioral
> results remain valid. See the explicit disk-backed rerun below; CI and the
> real synthetic runtime are separate evidence.

Date: 2026-10-19 (Asia/Aden). Issue: [#38](https://github.com/so7ob/MikroDash/issues/38).
This is a **development preview, not acceptance-complete**. Passing the checks below does not
establish live HotSpot sign-in, CoA speed verification, production readiness or deployment.
See [architecture and remaining work](Subscriber-Management.md) and the
[lab guide](Subscriber-Lab-Guide.md).

The focused missing-final-accounting safety correction has a separate
[contract and verification record](Subscriber-Final-Accounting.md).

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

Historical commands (native Go 1.27.2, linux/amd64; synthetic accounts and scripted
NAS reads). The earlier claim of ordinary SQLite temporary storage was incorrect
for commands using RAM GOTMPDIR; see the explicit disk-backed correction below:

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
  13.775 / 13.219 / 61.063 seconds on the development host. These are behavioral
  results; RAM GOTMPDIR also places SQLite T.TempDir files in RAM. They do not
  establish ordinary-storage persistence.
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
was retained; no check was skipped. Its RAM GOTMPDIR also affected SQLite test
storage, contrary to the earlier ordinary-filesystem description. The disk
briefly approached capacity during concurrent
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


## Display-name editing (Issue #52)

Base: verified `develop` `b897307b`. The actual HTTP regression first failed with
400 on the unsupported payload. The integrated handler/store then passed. This
was a behavioral red, not an initial compile failure.

- `go test ./internal/subscriber ./internal/server -run 'Test(AccountName|SubscriberName)' -count=1 -v`:
  exit 0, 0.370/0.212 s. Covers preserved snapshot/password/voucher values,
  Unicode and whitespace, before/after audit, stale/no-op edits, validation,
  rollback on audit failure, selected-account isolation and admin/Origin checks.
- `npx --no-install tsc --noEmit`: exit 0.
- Tagged `TestSubscriberAdminReconciliationBrowserAcceptance`: exit 0, 10.923 s
  after focused-form changes. The actual protected HTTP/SQLite journey covers
  cancel, validation retention, a real concurrent edit and 409, reviewed save,
  disabled busy controls, escaped Unicode, focus and 375/1280 dark/light/Nord with
  zero axe violations. Existing reconciliation acceptance still runs unchanged.

Only NAS data is synthetic. Screenshots are private in
`.screenshots/subscriber-name-52/`. Full final-head checks, mutation evidence and
pre/post-merge CI remain delivery gates; the name editor is not target acceptance.


The first full #52 sweep failed `TestEveryLookupHasAProducer`: the generated
input helper hid the literal `subEditName` producer from the static scan, while
the browser journey had passed. The field now declares its input ID explicitly,
using the same local form classes. The audit and all its assertions remain intact.
The failed full sweep is not reported as green.

## Confirmed HotSpot CoA address, Issue #55

Real isolated CHR 7.24.5 testing found that the original CoA selectors received
Unsupported-Extension. Adding the API-confirmed Framed-IP-Address produced ACK
and actual timeout/rate readback; see [the detailed evidence](Subscriber-CoA-Targets.md).
The private raw successful journey ran Start, a standard 60-second Interim and
Stop; final counters were 63 seconds, 66746 download and 18027 upload bytes.
The original compile error, selector NAKs and rerun-with-existing-session failure
remain retained, separately from the successful probes. No L009 write was made.

`go test ./internal/subscriber ./internal/subscriberlab ./internal/guard` on the
development checkout passed after the production correction (7.497 s / 0.007 s /
cached). The subscriberlab package is separate work under #54 and is not part of
this focused fix. `go test ./internal/subscriber -run
'Test(CoA|IssuedCoA|FiveContinuous)' -count=1` passed in 0.142 s. Full exact-source,
mutation and CI delivery evidence follows only after those checks complete.

Focused delivery source: `84c1bee9fd89dc3ddc6bdd595e1900a07d32d78a`.
The isolated worktree's `sh tools/verify.sh` passed: gofmt, vet, 62 Go packages,
live govulncheck, all generated contracts, tsc, 146/146 frontend tests and 22/22
tooling tests. Raw Docker output was retained; only the compiler cache and
GOTMPDIR moved to RAM. Go 1.27 also placed T.TempDir SQLite files there;
the earlier ordinary-filesystem description was incorrect. The explicit
disk-backed rerun below corrects that evidence. The task-created node_modules symlink was removed after checking.

`go test -race ./internal/subscriber -run
'Test(CoA|IssuedCoA|FiveContinuous)' -count=1` passed in 2.270 s. Two compiling
mutants were killed: omitting the Framed-IP-Address setter and bypassing the
processor's issued-address fence. Their specific regressions failed; neither
was a compilation failure. The working source was restored byte-for-byte.
Raw private logs: `/tmp/subscriber-55-full.log`,
`/tmp/subscriber-55-raw-docker.log`, `/tmp/subscriber-55-race.log` and
`/tmp/subscriber-55-mutation-*.log`. No browser UI changed in this correction;
existing frontend coverage passed. Required PR and post-merge CI remain separate
checks and are recorded in the issue after delivery.


## Confirmed HotSpot Disconnect address, Issue #57

Base: verified develop `16deeb1b`. The new production-call-site UDP regression
first failed because the emitted packet had no Framed-IP-Address (both ACK/NAK
subcases, behavioral failures). After correction, `go test ./internal/subscriber
-run 'TestDisconnect|TestGuardedBackgroundDisconnect' -count=1` passed in 3.282 s.
Tests cover exact selectors, observed identity/freshness/address refusals,
service authority, ledger ownership, audit failure and retained entitlement.

The isolated CHR 7.24.5 HTTPS HotSpot probe first retained the production failure
on the base revision. Adding only the observed IPv4 in a diagnostic packet
produced Disconnect-ACK and Admin-Reset Stop. Running the corrected production
method with a Go source overlay then passed: API disappearance plus committed
Stop, 0 pending accounting receipts, one 77.169 ms sample, exit 0. The overlay
replaced only `internal/subscriber/runtime.go`; the private #54 lab harness and
fresh synthetic ledger remained separate from this fix's delivery scope.
No operator NAS configuration, capability flags or deployed process changed.

Full final-source verification, mutation/race checks and current-head/post-merge
CI remain separate delivery gates. L009 acceptance and performance budgets are
not inferred from the single CHR sample.


The initial full sweep passed 62 Go packages, 146 frontend tests and 22 tooling
tests, all generated contracts and live govulncheck. Scoped race tests passed
in 15.278 s. The missing-address mutant failed behaviorally. A first stale-read
mutant survived because the test's timestamp also mismatched the session start;
that result is retained. The fixture now preserves a valid start-time match,
and the same compiling mutant fails with `unconfirmed target accepted`. Final
source verification follows this test correction; the earlier sweep is not
reported as checking the corrected assertion.


## Explicit disk-backed verification correction, Issue #54

Go 1.27's testing source documents and implements GOTMPDIR as the parent of
T.TempDir. Setting only TMPDIR does not override that. Earlier local runs with
RAM GOTMPDIR are behavioral tests, not evidence of disk persistence or fsync
cost. The real runtime ledger remains under the ordinary development filesystem;
GitHub CI did not set GOTMPDIR to a RAM mount. No source assertion is weakened.

The subscriber source exactly matches verified develop `7d6f8121`. It was
compiled as a race-enabled test executable in RAM, then run separately:

```sh
go test -race -c -o /path/to/ram/subscriber.test ./internal/subscriber
GOTMPDIR=/tmp TMPDIR=/tmp /path/to/ram/subscriber.test -test.count=1 -test.v
```

The compile command alone may use RAM GOTMPDIR. The test process must use an
ordinary filesystem directory for **both** variables. The actual rerun passed
all 64 top-level subscriber tests/fuzz seed checks, including receipts, crash
recovery, CoA, Disconnect, accounting boundaries and concurrent reservations.
Actual database startup logs show `/tmp/Test.../mikrodash.db`. Randomized fuzzing
is separate from these seed checks. The raw private log is
`/tmp/subscriber-57-all-disk-race.log`. A focused disk race run also passed.

The complete subscriberlab test executable passed separately with the same
disk settings; actual preparation logs show `/tmp/Test.../runtime-data/`.
It exercises the real encryption/ledger/RBAC preparation call site and refuses
repeat preparation without replacing accounts. Private log:
`/tmp/subscriber-54-disk-unit.log`. New runtime exhaustion helpers added after
that binary was compiled still require their own final-source checks.


## Finality and CHR continuation verification

Issue #59 / PR #60 delivered account-wide missing-final accounting gates at
`develop` `3bfdbd25`. The reviewed head `1abd51de` and merged trees match;
[head CI](https://github.com/so7ob/MikroDash/actions/runs/39376922603) and
[post-merge CI](https://github.com/so7ob/MikroDash/actions/runs/39377365380)
passed. Its complete standalone subscriber race run passed 70 top-level
checks on ext4 `/tmp`; all six compiling finality mutants failed relevant
behavioral assertions. The first restart fixture's Store-pointer race was
repaired by draining the old receiver before storage reopen, without weakening
production synchronization.

A supplemental 20-second standalone accounting fuzz run passed 33 executions
in about 21 seconds on that source. Go warned that the binary lacked coverage
instrumentation; it is random input testing without coverage guidance. The
first invocation lacked `-test.fuzzcachedir` and executed no fuzz cases. Both
logs are retained. Do not equate this with a coverage-guided fuzz campaign.

The CHR continuation at `679ad9fd` passed the full local sweep: 63 Go packages,
gofmt/vet/govulncheck and current generators, TypeScript, 146 frontend and 32
tooling tests. The Docker wrapper applies `-count=1` and an execution environment
with both GOTMPDIR and TMPDIR on ordinary `/tmp`; only compilation work/cache
uses RAM. A separate subscriberlab race executable passed all 11 top-level
tests on ext4 `/tmp`, including actual encrypted runtime-data/RBAC preparation.

Subsequent isolated assertions for the process binary hash passed and killed
a compiling omission of that fence. Six Node mutations cover PID start ticks,
argv, binary/config hashes, app-before-VM teardown and the isolated listener
firewall. Four compiling Go overlay mutations cover administrator grant at its
preparation call site, malformed durations, mixed certificate material and
failed evidence writes. Every mutation fails a behavioral assertion; syntax or
compilation failure was not counted as a kill.

The clean native application at `1abd51de` also passed the public actual-client
`reconciling` browser case against the retained CHR outage ledger: positive known
credit, pending final accounting, refused HTTPS HotSpot re-entry, retained
confirmed balances and safe portal access. Three themes at 375/1280 pass LTR,
overflow and axe checks. This is not an L009/C or automatic landing result.
Final #54 head and post-merge CI remain separate delivery gates.


## Checked HotSpot duration observations, Issue #62

Develop `3bfdbd25` fails the real shared observer call-site regressions: malformed
uptime becomes a complete snapshot and closes a trusted active lease; nonempty
garbage timeout becomes known zero. The corrected native source at `7adcc155`
passes the focused server race executable with GOTMPDIR/TMPDIR on ext4 `/tmp`,
including preservation of the exact trusted ledger snapshot. Parsing and byte
values use fixed-width checked quantities; the collector legacy helper is
unchanged. The first race build failed for lack of task RAM build space before
testing, then succeeded after only task-built executables and old task compiler
cache entries were reclaimed; the manifest and failure output are retained.

The arithmetic fuzz property compares canonical duration components against
independent big.Int sums. A 10-second standalone race run with two workers
passed 121505 executions in about 11 seconds. Its binary lacked coverage
instrumentation, so this is random testing without coverage guidance. Six
compiling overlay mutants fail relevant assertions: partial parsing, sum-bound
omission, timeout nonempty coercion, permissive uptime call site, skipped bad
active row and lost explicit zero. No compilation failure counts as a kill.

The actual namespace browser against the same owned CHR 7.24.5 and clean native
source passes HTTPS login, exact-device speed readbacks/cooldown and all six
responsive/theme axe checks. A later normal-worker observation has known time
and byte fields at 74 connected seconds with a verified renewal; owned HTTPS
logout follows. Malformed and ten-year/overflow cases are scripted inputs,
not hardware observations. Final full gate and exact-head/post-merge CI remain
required before Issue #62 delivery.

## Owned connection entry verification (Issue #64)

Application revision `85817872485d8df91d18154c430c971362b758da` passed
[PR CI](https://github.com/so7ob/MikroDash/actions/runs/39515748159).
Local verification used an exact `git archive` of that revision: 2221 tracked
files were byte-checked against the visible issue checkout. The snapshot excludes
ignored operator data, matching the CI source universe. It is not another
development checkout. A prior root sweep refused the existing ignored GeoIP
`cities.json` because it exceeds the credential scanner's size bound; that
failed sweep is retained. No GeoIP data was deleted and no scanner was weakened.

Go 1.27.2 formatting, `go vet -p 2 ./...`, and
`go test -p 2 -count=1 -exec <ordinary-disk-wrapper> ./...` passed all 64 tested
Go packages. The wrapper sets both `GOTMPDIR=/tmp` and `TMPDIR=/tmp` for test
processes; compiler caches may remain in RAM. Govulncheck found zero called or
imported vulnerabilities, with one unused required-module advisory. Six Go
generators and seven generated frontend tables were current.
`sh tools/verify.sh --no-docker` passed TypeScript, 150 frontend tests and
32 tooling tests. Its Go half was explicitly skipped by the wrapper and run
natively immediately beforehand; this does not claim a Docker verification run.

Focused native cgo race instrumentation passed in subscriber (5.483 s), server
(10.851 s) and subscriberlab (1.021 s), selecting the new connection, ticket,
outcome, handoff and installer tests. Four compiling overlays killed their
intended invariants: foreign NAS ticket consumption, stale confirmation,
Access-Accept escaping failed outcome persistence, and skipped asset readback.
Compilation failures do not count as kills. The first NAS-result classifier
expected an earlier assertion; the retained test instead correctly failed on
ticket consumption, and its verified classification records that distinction.

Actual coverage-guided `go test -fuzz` runs used two workers and 15 seconds each:
accounting monotone replay: 248 executions; receipt ranges/retry identity:
100314; renewal time-budget conservation: 155. All passed. These are instrumented
fuzz runs, separate from earlier standalone random testing without coverage.
Private commands, raw logs and overlay receipts remain in
`.temp/subscriber-64-recovery`; they contain no target-router acceptance claim.

## Issue 66 calendar delegation verification

Implementation source: `82f52e99` on `feat/66-calendar-boundary-renewal`, PR #67.
All development was in the visible checkout. The full verification archive was
created from that exact commit and byte-matched 2232 tracked files; it omitted
ignored operator runtime data, including a pre-existing oversized GeoIP source
that the root credential scanner correctly refuses. No scanner was disabled and
no operator data was removed. Mutations ran only in the archive and every tracked
byte was restored. The final test/documentation follow-up does not change the
production implementation used by the CHR run.

| Check | Actual result / environment |
| --- | --- |
| Native full format/vet/test | Passed, Go 1.27.3 linux/amd64; 64 tested packages |
| `govulncheck@latest ./...` | Go 1.27.2 failed GO-2026-6809; Go 1.27.3 passed with zero reachable vulnerabilities; an uncalled module advisory remains diagnostic |
| `go run ./cmd/webbuild -dir web` | Passed |
| `sh tools/verify.sh --no-docker` | Passed generators, tsc, 7 generated tables, 150 frontend tests and 32 tooling tests; its skipped Go half was separately executed above |
| `go test -race ./internal/subscriber -count=1` | Passed 82.738 s on implementation with Go 1.27.2 |
| Expanded five-anchor authenticated UDP scenario under `-race` | Passed 4.054 s on Go 1.27.3 |
| `go test ./internal/subscriber -run '^$' -fuzz '^FuzzCalendarAdmissionPeriodConservation$' -fuzztime=30s -parallel=2` | Passed 77040 executions, independent interval-conservation oracle |
| Six compiling isolated mutants | All killed by behavioral failures; no compile failure counted |

Go compilation used a RAM build cache/temp directory. Test execution used an
`-exec` wrapper setting both TMPDIR and GOTMPDIR to ordinary `/tmp`, as required
by the earlier storage correction. These checks do not prove power-loss media
durability. The official Go 1.27.3 archive checksum was verified and installed
alongside the preserved old toolchain. The owner F5 process was never signalled.

Mutants removed the legacy retry commit, early-start coverage, exact byte endpoint,
combined renewal rate verification, suspension replay guard and maximum prior
period exposure. Each produced a behavioral assertion failure in its focused
regression. Raw commands, fail/pass logs and source/restoration proof remain in
private `.temp/subscriber-66-recovery`; no credentials enter public fixtures.

### Actual owned CHR result

An independent explicitly synthetic account and immutable hourly plan ran through
the actual namespace client's HTTPS portal, owned ticket, HTTPS HotSpot login and
normal MikroDash background worker on CHR 7.24.5. The existing unresolved account
was retained. The pilot allowlist contained only the new fixture, in a cloned
private configuration. The first attempt correctly refused missing host-table
presence, with zero issued leases. A documented v2 reschedule and actual HotSpot
landing corrected the fixture without changing earlier usage.

With no browsers or WebSocket watchers, the same live session crossed the actual
hourly reset and five verified renewals in 202.294 seconds of observation. At the
last live reading, 203 seconds split into 90 before and 113 after the reset. Logout
and trusted final accounting committed 204 seconds, 66055 download bytes and 1924
upload bytes, with zero pending accounting. A 65536-byte application payload took
3.990 seconds after the reset; all download usage belonged to the new period.
The lifetime/bucket totals conserved the final counters, and the earlier account's
counters, subscription and unresolved finality were unchanged.

This is actual CHR protocol/runtime evidence, not L009/C Internet acceptance.
It does not measure bytes inside a sampling gap, captive-browser compatibility,
under-load resource budgets or a guaranteed update/disconnect latency. The C-device
owner separately confirmed existing-router-CA portal HTTPS without warnings, and
the live chain/admin-route isolation was verified; C remains Shadow with Internet
admission disabled. Current PR-head and post-merge CI are required separately.
