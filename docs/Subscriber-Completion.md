# Subscriber Management completion ledger

This ledger tracks the full completion contract and the original Subscriber Management
requirements. It does not replace them. Baseline: `4ed68ee3`, Issue #38, existing visible
checkout. Foundation PR #39 landed in `develop` at `430337cb`; session renewal and
observation continue independently in Issue #40. No live router writes or production cutover are authorized by this ledger.

States: `partial`, `implemented`, `passed`, `pending`, `blocked`. Automated evidence does
not imply target-router verification. All target-router acceptance remains pending until
a specifically authorized C pilot is executed. Hardware details in older reports are snapshots.

| ID | Required behavior / acceptance | Current evidence and actual gap | Dependencies / affected implementation | Code | Automated | Target / blocker |
| --- | --- | --- | --- | --- | --- | --- |
| AC-01 | 0/3600 before and after first connection | Accounting regression; fast observations pending | subscriber accounting/storage, portal | partial | prior pass | pending: C pilot |
| AC-02 | Reconnect preserves 2400; 109+51 leaves 3440 | Durable ledger regression | accounting | implemented | prior pass | pending: C pilot |
| AC-03 | Five continuous renewals, crash after application | Durable total-session CoA renewal implemented; five UDP/virtual-clock cycles, real normal-worker CHR cycles through 201 seconds and a fresh 19-operation post-browser run, and scripted post-NAS/pre-ACK crash recovery pass. Total-session semantics remain explicitly required; calendar continuity now passes five-anchor ledger/UDP and actual CHR hourly-reset acceptance, while target acceptance remains open | operations, reservations, verified timeout semantics | partial | passed: scoped UDP/crash tests | pending: scoped timeout probe |
| AC-04 | Concurrent daily/monthly time/byte limits | Pure/ledger tests; actual CHR time/download/upload and hard download exhaustion pass, including refused re-entry; daily/monthly target probe remains | policy/accounting/runtime | partial | passed: ledger + CHR scoped exhaustion | pending: C traffic test |
| AC-05 | Live rate switch/throttle, correct device | Real UDP rate switch and active throttle with exact-device queue readback pass; burst/minimum/priority/network ceilings pending | persistent CoA operations, queue readback, capabilities | partial | passed: UDP + queue readback | pending: rate probe |
| AC-06 | Reordered/retried accounting, reused IDs | Durable partial receipts, exact retry identity, complete-Stop coverage and late partial/final recovery implemented; target retransmission capture remains | radius/accounting | partial | prior pass | pending: retransmission capture |
| AC-07 | API/RADIUS unified debit, one-second skew | Shared capability-bound API observations use the cumulative ledger; overlapping older RADIUS sample regression passes | observation matching and accounting | partial | passed: shared ledger regression | pending: direction proof |
| AC-08 | Missing Stop/restarts reconciled safely | Complete API absence closes the seat after issued bounds; unresolved exposure remains a quota hold until covering Stop. Audited admin settlement and explicit byte-gap allocations implemented; incomplete telemetry receipts/review implemented, including physical SQLite close/reopen and late partial counter corrections; missing-final accounting now gates new admission and renewal until trusted Stop or audited review, including zero-hold sessions | absence evidence and administrative settlement | partial | passed: absence/Stop holds | pending: restart probe |
| AC-09 | Calendar anchors and byte-gap settlement | Days 29/30/31 clamp without drift, leap tests pass; approved byte-gap settlement implemented; continuous hard-edge renewal implemented with independent reservations; actual CHR hourly edge passed, target remains | policy, journal adjustments | partial | passed: calendar anchors and approved settlement | pending: boundary observations |
| AC-10 | Shared quota and same-NAS networks | Atomic admission tested; same-source networks map by authenticated Called-Station-Id/Class; UDP forgery regression passes | radius network mapping, renewal locking | partial | partial | pending: multi-device C/D lab |
| AC-11 | No browsers required | Actual startup and watcher-free observer checks for both portal transports; background Disconnect UDP test; fresh real CHR session retained through 19 verified renewals after all browsers closed | explicit runtime lifecycle and observer | partial | passed: scoped lifecycle/UDP | pending: closed-browser run |
| AC-12 | Local outage bounds and fail-closed admission | Actual CHR session with 26 seconds left disappears during a 91.679-second service outage; restored ledger retains pending finality, closes seat and denies new admission with known positive credit. Directional cap has measured nonzero excess; operational budget unapproved | verified renewal and local limits | partial | passed: scoped CHR outage/refusal | pending: C outage probe |
| AC-13 | Single C sign-in and safe exhausted portal | Actual automatic CHR landing, one credential submission, HTTPS browser POST, owned return and verified rates pass; exhausted and missing-Stop refusal both return automatically to the safe account. Six viewport/theme/axe checks per journey pass; L009/C and OS captive browsers pending | actual client HTTPS POST and verified session | partial | passed: scoped actual CHR + synthetic transports | pending: client behind C |
| AC-14 | Ownership/device/network/ticket isolation | HTTPS/HTTP/SQLite/UDP tests cover peer, forwarded-client, device, ticket, admin-route and WebSocket isolation | expanded runtime/operations security | partial | prior pass | pending: C isolation |
| AC-15 | Authenticators, commit-before-reply, faults | UDP forgery/unknown-source/storage fault tests, strict presence/duplicate/range validation and commit-before-reply receipts implemented | crash/rollback and operation replay tests | partial | partial | pending: target protocol |
| AC-16 | Complete admin/portal/browser workflows | Four tabs including tested reconciliation and display-name editing; broader account/credential editors, search/bulk/history/setup still pending | backend edit/search/bulk/history/simulator/setup, UI | partial | partial | pending: final browser journey |
| AC-17 | Idempotent import/opening/cutover/rollback | Read-only User Manager preview | transactional mapping/apply/approval | partial | missing | pending: approved live batch |
| AC-18 | Measured freshness/CoA/disconnect/resources | Duplicate transaction benchmark only | shared observer and lab load harness | partial | missing | pending: target load approval |
| AC-19 | Target build and existing app compatibility | Verified develop CI, prior ARM64 build and clean 27d17730 ARMv7 static build; target reports arm, execution pending | build/regression gates and isolated data | partial | prior pass | pending: target build |

## Capability matrix

| Capability | Official reference | Implementation baseline | Lab | Target |
| --- | --- | --- | --- | --- |
| RADIUS PAP/Class/accounting | RouterOS RADIUS; RFC 2866 | implemented | UDP tests passed | pending |
| Session-Timeout at admission | RouterOS RADIUS | implemented, max 60 seconds | attributes tested | enforcement pending |
| Session-Timeout CoA total/remaining meaning | RouterOS lists writable attribute; meaning needs probe | durable total-session implementation; confirmed IPv4 selector | real CHR 7.24.5 total mode, extension past 60 seconds and immediate shortening proved; normal worker retained the same RADIUS session for 19 post-browser verified renewals | pending |
| CoA rate change | RouterOS RADIUS Change of Authorization | durable rate operations, confirmed IPv4 and capability gate | real CHR exact-device dynamic queue changed twice through the normal worker and browser path | pending |
| Dynamic queue identity/rate verification | RouterOS queue and HotSpot active menus | exact unique dynamic host queue parser | parser + rate readback passed | pending |
| API HotSpot octet direction | Conflicting service-specific wording requires one-way traffic | explicit per-source capability mapping | real CHR one-way traffic and RADIUS/ledger agreement: input upload, output download | target one-way probe pending |
| Shared NAS source distinguished by HotSpot server | Called-Station-Id documented as HotSpot server | authenticated Called-Station-Id and owned Class mapping | UDP isolation passed | pending |
| Interim >=60 seconds | RFC 2869 section 5.16 | implemented | attribute test passed | local 5s compatibility unverified now |
| HTTPS edge with isolated private HTTP portal | RouterOS Reverse Proxy | explicit peer/Host/protocol/forwarding boundary, native TLS default | actual TLS/HTTP/SQLite/browser/UDP passed | header/device/route acceptance pending |

References: [RADIUS](https://manual.mikrotik.com/docs/authentication-authorization-accounting/radius/),
[HotSpot](https://manual.mikrotik.com/docs/authentication-authorization-accounting/hotspot-captive-portal/),
[RFC 2866](https://www.rfc-editor.org/rfc/rfc2866), [RFC 2869](https://www.rfc-editor.org/rfc/rfc2869),
[RFC 5176](https://www.rfc-editor.org/rfc/rfc5176). Documentation is not target proof.

## Progress and resume

- Initial audit: verified existing immutable plans, atomic admission, Class-bound cumulative
  accounting, isolated portal, 2-second worker, and narrow Disconnect guard. Confirmed missing
  renewal, rate application, shared observation and automatic settlement. Work begins with
  persistent session operations and shared observation; later AC rows remain in scope.
- Delivery sequence: foundation PR #39, independent session/observation Issue #40,
  then provisional exposure reconciliation Issue #41. Each issue branch starts from
  current `develop` and requires its own PR, exact-head CI and post-merge CI.

### Continuation checkpoint: session operations and observation

Implemented additive migration 39, durable CoA intent/retry/readback state, service change
grants, own-reservation-aware renewal, shared API observations, a single cumulative debit
path, explicit source timestamps, exact-device queue readback, active throttle application,
absence settlement with unresolved quota holds, same-NAS network routing, and clamped
calendar anchors. Selection does not reset subscription identity, validity, usage or quota.
The portal shows observed connection state and applied queue ceilings separately.

Actual verification (development host, isolated synthetic accounts):
- `go test ./internal/subscriber ./internal/guard ./internal/db`: exit 0.
- `go test ./internal/server -run 'Test(Portal|ShadowCannot|Subscriber)' -count=1`: exit 0.
- `go test -race ./internal/subscriber ./internal/guard -count=1`: exit 0; 48.359 s / 2.639 s.
- TypeScript and 142 frontend tests: exit 0. Frontend build: exit 0.
- Tagged `TestSubscriberBrowserAcceptance`: exit 0; actual Chromium, synthetic TLS/SQLite,
  login/choice/shadow refusal/logout, 375/1280, three themes and axe. No target HotSpot involved.
- Timeout extension, shortening and relative-mode refusal tests pass. The current reservation
  model enables only proven total-session CoA semantics; relative delayed retries need more
  implementation. Hard calendar-edge continuous renewal is also still a code gap.

Read-only target refresh at 2026-10-19T20:37:38Z found RouterOS `7.24.5 (stable)`,
`L009UiGS-2HaxD`, clock `manual` with `+00:00` offset, and no unavailable readiness menus.
This contradicts assuming the old Asia/Aden router setting is current. No clock or other
router setting was changed. Raw topology remains private outside the repository.

A compiler-cache expansion reduced free disk below 300 MiB. Only exact rebuildable cache
artifacts created during this goal were removed; data directories, volumes and operator files
were preserved. The earlier Alpine race attempt could not run without cgo; it is not a pass.
The native compiler race run above replaced it. A multi-network fixture race was fixed by
configuring clients before starting UDP handlers; no production lock was weakened.

Remaining independent work continues with auditable administrative reconciliation, byte-gap
settlement and the administration/simulator workflows. The original AC-01..AC-19 scope remains
active; this checkpoint is not implementation completion or target acceptance.

### Delivery review for Issue #40

The full-suite command-source census caught a missing diagnostics label for the new
observer; it is now named in `commandSources`. This was a real failing gate and was
not waived. Persisted CoA packets also recheck current capability evidence before
retry, including total/gigaword support. Revocation regressions cover both rate and
renewal packets. Unsupported relative timeout/byte encoding is refused explicitly.

Focused `go test ./internal/subscriber ./internal/guard ./internal/db` passed after
this review. Full exact-source verification and PR/post-merge CI are recorded on
the scoped PR; the earlier `93aa15fe` checkpoint is not reported as fully verified.
The host's disk constraint was mitigated by removing only task-created rebuildable
compiler-cache entries and using a RAM directory for subsequent compiler caches.
Earlier local RAM GOTMPDIR also placed test databases and journals in RAM;
see the explicit ordinary-storage rerun in Subscriber-Verification.md.

### Independent delivery checkpoints

- Foundation: PR #39, `develop` `430337cb1fca53467d7393c09536c7766a344a95`;
  [exact-head CI](https://github.com/so7ob/MikroDash/actions/runs/39309462140) and
  [post-merge CI](https://github.com/so7ob/MikroDash/actions/runs/39332843908) passed.
- Session renewal/observation: Issue #40 / PR #42, `develop`
  `8931effdda23da84d14c340ea5158bbe37caf8eb`;
  [exact-head CI](https://github.com/so7ob/MikroDash/actions/runs/39333701987) and
  [post-merge CI](https://github.com/so7ob/MikroDash/actions/runs/39334260634) passed.
  Exact implementation head `8225eff7` also passed full local verification,
  race checks, synthetic browser acceptance, a capability-revocation mutation and
  a static ARM64 cross-build. Native supplemental race/browser runs used tmpfs;
  the earlier ordinary-filesystem description for local RAM GOTMPDIR commands
  was incorrect. Later explicit disk-backed reruns are recorded in verification.
  Neither proves media durability under power loss or target-router execution.
- Reconciliation: Issue #41 on its independent branch from `develop`. Migration 40,
  provisional exposure, transactional audited approval APIs and the fourth admin
  tab are implemented. Focused ledger/API checks and actual HTTP/SQLite Chromium
  journey passed on the working source. Full PR-head/post-merge evidence remains
  pending until this milestone's delivery. See [reconciliation](Subscriber-Reconciliation.md).

Browser overflow was traced to a synthetic shell missing MikroDash's actual `#main`
flex container. The harness now uses that container. The four product tabs wrap with
existing utilities and are individually checked within the mobile viewport. Approval
returns focus after removing its form. No new font, direction, global palette or
localization framework was introduced. Full module and target acceptance remain open.


The first full reconciliation sweep stopped at `TestTemplateIDsAreBound`: its
literal scanner cannot resolve the three per-record field prefixes read through
`data-fields` and the shared submit handler. Their actual delegated bindings are
now recorded in the existing bidirectional constructed-ID ledger, as other dynamic
fields are. The check remains enabled, and its stale-entry/empty-set assertions
are unchanged. This initial full sweep is a failed run, not a passing result.


### Reconciliation delivery and renewal precision

Issue #41 / PR #43 landed in `develop` at
`79751394fd8c4da67f0ad7b9ad443f2043b053f3`.
[Exact-head CI](https://github.com/so7ob/MikroDash/actions/runs/39336445962) and
[post-merge CI](https://github.com/so7ob/MikroDash/actions/runs/39336946479) passed.
The full-module Issue #38 remains open.

Issue #44 proceeds independently from that verified base. Its new regression first
failed: one/five seconds of observation delay encoded 61/65 seconds for a 60-second
time entitlement. The correction anchors cumulative timeout to trusted NAS uptime,
verifies uptime/time-left rather than API completion time, and atomically fences
legacy pending intents while retaining maximum issued exposure. Package tests pass,
including legacy audit rollback and shared admission. Full local/PR/post-merge
verification for #44 is pending until delivery; target proof remains pending.

Known software follow-ups still include incomplete RADIUS payload receipts, hard
calendar continuity, relative-mode replay safety, complete policy/admin/import/setup,
and large
pending-operation queue recovery. These are part of the original contract, not
hardware-only acceptance exceptions. No completed full-module claim is made.


The first #44 full/race sweeps failed in the newly added fuzz seeds. The internal
protocol fixture still concatenated a file URI; `seed#0`, `seed#1` and `seed#2`
therefore collided at the URI fragment delimiter. Production `db.Open` already
escapes literal paths. The fixture and its benchmark now construct an escaped URL,
so every seed uses its own actual SQLite file. No data was deleted to make the test
pass. The failed sweeps and the separately passing 32-execution short fuzz probe
remain distinct results; full/race verification is rerun on the corrected source.


### Administration-first trial (Issue #47)

The running development command was inspected on 2026-10-20 (Asia/Aden): it has
no `-subscriber-config`, and its data directory contains no subscriber configuration
or portal TLS assets. Only the loopback admin listener is running. This explains
the reported disabled error; merging code does not activate a runtime.

The protected administration path now uses the existing auditDB subscriber ledger
without requiring the optional RADIUS/portal service. Real HTTP/SQLite regressions
exercise account creation with Unicode, immutable plans, simultaneous time/download
limits, first-login assignment, snapshot, status changes and reconciliation. They
also assert that no runtime is started and retain auth/Origin/storage refusal.
A new regression reproduced the former HTTP 503 before the correction.

The latest read-only NAS inspection at 2026-10-19T23:34:37Z found RouterOS
7.24.5 (stable), L009UiGS-2HaxD, Asia/Aden (+03:00), HTTPS/RADIUS/accounting on C,
incoming RADIUS on 3799, disabled IPv6 and no enabled FastTrack rule. A and B
still do not use RADIUS. This is a readiness snapshot, not target acceptance.
The current application is a development host with a loopback-only HTTP listener;
client-to-portal and NAS-to-application UDP reachability are not established.
No certificate, firewall, RADIUS order, existing account or runtime was changed.

A real C trial still needs an exact scoped HTTPS/NAS configuration and rollback,
approved live writes and a client behind C. Local-limit and CoA semantics must be
measured before their capability flags can be enabled. The accounting-receipt
follow-up (#46) is preserved on its independent branch; it is not included in
this setup correction. Full-module Issue #38 remains open.

Source audit correction: the portal already separates business Remaining from
Available for new connections. The earlier follow-up list overstated that gap.
Full local/PR/post-merge evidence for #47 will be recorded at delivery; this entry
does not claim a live HotSpot acceptance result.


### HTTPS reverse proxy transport (Issue #49)

Previous goal turn: progress. PR #48 delivered administration-first setup at
`develop` `fe0db269d7d2b2dc7c8bbf087998bb2c2f26b497`; its exact PR head and
post-merge CI both succeeded. The operator confirmed that the reported disabled
message appears in the current VS Code development process. Restarting that
process loads the new administration handlers; no runtime was restarted here.

An independent numbered branch now implements an exact-peer private HTTP portal
behind the target's existing HTTPS proxy model, retaining native TLS by default.
Config/sealing, all-route boundary, right-to-left client attribution, device-bound
tickets, actual startup/shutdown and browser/WebSocket isolation are integrated.
The initial config regression reproduced the TLS-only failure. Boundary tests
also found IPv4-mapped unspecified addresses; normalization now refuses them.
A scripted-session cleanup initially tried live collector teardown and panicked;
the fixture now cleans up only the resources it actually owns.

Actual focused evidence: HTTP/TLS/SQLite plus owned RADIUS over UDP passed;
startup/observer-without-watchers/shutdown passed for both transports. Native race
checks passed in 8.807 seconds. A 5-second forwarding fuzz probe passed with
14,939 executions. Actual Chromium passed both transports in 14.373 seconds,
including mobile/desktop, three themes and axe. No target traffic is simulated
as hardware proof. Full local/PR/post-merge verification remains a delivery gate.

The actual target reported architecture `arm`; a static ARMv7 binary was built
at the prior tested PR head but was not executed on the target. Its existing
container metadata points to an older image and uses external USB storage.
Two active C sessions were observed in the private read-only transport snapshot.
No container, proxy rule, certificate, session or RADIUS authority was modified.
Private endpoint values and rule identifiers remain outside public documentation.

This closes a software transport prerequisite, not the full C pilot. Accounting
receipt completion (#46), continuous calendar renewal, remaining policy/admin/import
and setup work, plus target acceptance and measurements remain required. The
complete AC-01..AC-19 scope stays active. See [proxy guide](Subscriber-Proxy.md).


Before delivery, the limiter call-site regression also passed, and both targeted
trust/identity mutations were killed by real assertions. The first full sweep
reported a Go failure with no useful filtered cause; the unchanged traced Go half
passed. A final full sweep retains raw Docker output to preserve a future cause.
No root cause is claimed from an absent diagnostic. Current ARMv7 source built
successfully at `b200018c`; target execution still needs approved deployment.


### Durable accounting receipts (Issue #46)

Issue #49 / PR #50 delivered the trusted proxy transport to `develop` at
`baae4f3e3c8bcf9565f97028684b12f1fd8b91eb`. Both
[exact-head CI](https://github.com/so7ob/MikroDash/actions/runs/39347203885) and
[post-merge CI](https://github.com/so7ob/MikroDash/actions/runs/39347735715) passed.
The final full local sweep passed all 62 Go packages, 145 frontend and 22 tooling
tests. The additional clean ARMv7 build records `vcs.modified=false`; target
execution is still pending.

The preserved #46 work is resumed on its independent branch from that verified
base. Migration 41, original whitelisted receipt evidence, missing-versus-zero
statistics, durable pending acknowledgment, unified final/API coverage, admission
and operation fences, reasoned final corrections and existing-panel review are
implemented. New regressions reproduced malformed Start accepted as zero, distinct
malformed evidence collapsing and a new late partial Stop blocking a settled
account. These defects were corrected without dropping or clearing usage.

Focused subscriber/database/server packages passed (13.775/13.219/61.063 s).
TypeScript and 146 frontend tests passed. Native receipt race tests passed in
9.065 s; a five-second exact-counter/retry fuzz run passed 37,000 executions.
The actual protected HTTP/SQLite Chromium review journey passed in 4.566 s,
including original evidence, a corrective final reading, retained balances,
Unicode, 375/1280 and axe. These are synthetic lab results. Full exact-head
verification and PR/post-merge delivery remain required for this milestone.

No Internet admission, local-limit/CoA target claim or live configuration change
is inferred from these tests. The remaining original AC rows remain in scope.


Implementation `296a7bb5` passed the full repository gate: 62 Go packages,
146 frontend and 22 tooling tests, generated contracts and live vulnerability
checking. Administrative Chromium coverage also passed default dark/light/Nord
at 375/1280 (7.316 s). Native-TLS and trusted-proxy portal journeys passed clean
and pending-receipt accounts (13.551 s), retaining 20 used seconds and 3580
remaining. Early-success and API lifecycle-authority mutations were killed;
source was restored exactly. See the verification record for commands and limits.
This evidence does not close unimplemented #38 requirements or target acceptance.


### Display-name editing (Issue #52)

Issue #46 / PR #51 delivered durable receipts to `develop` at
`b897307b9ac2a953ab9334826c22ae94c41f6f01`; both
[exact-head CI](https://github.com/so7ob/MikroDash/actions/runs/39352049502) and
[post-merge CI](https://github.com/so7ob/MikroDash/actions/runs/39352549908) passed.
Clean static amd64 and ARMv7 builds at `b1efa3c2` record `vcs.modified=false`;
their tree equals the merge tree. No router deployment was performed.

The independent #52 branch implements selected-account display-name editing,
expected-name conflicts and atomic before/after audit, with the existing protected
administrative API and inline English/LTR form. Store and actual HTTP/SQLite tests
passed in 0.370/0.212 s. The final focused Chromium journey passed in 10.923 s,
including cancel, invalid/stale input retention, actual concurrent HTTP changes,
busy state, Unicode escaping, keyboard, dark/light/Nord, 375/1280 and axe.
TypeScript passed. Full final-revision verification and PR/post-merge delivery
remain required. No username/password edit, search, bulk or completed #38 claim
is inferred from this limited name-editor milestone.

### Issue #55: confirmed HotSpot CoA target

The isolated real CHR work under #54 discovered that CoA without the observed
Framed-IP-Address received Unsupported-Extension. The focused production fix
adds only the fresh owned-device IPv4 and fences persisted packets before retry
or readback against an absent/changed address. Credit and identity remain intact.
The five-cycle UDP NAS now requires the address, and processor replay, packet
and race regressions pass. See [target semantics and actual CHR evidence](Subscriber-CoA-Targets.md)
and [verification](Subscriber-Verification.md). The real CHR normal-worker,
HTTPS portal, byte-limit and target L009 acceptance remain pending; this does
not close #38 or the broader #54 lab.


### Issue #57: confirmed HotSpot Disconnect target

The isolated CHR production Disconnect probe exposed a missing Framed-IP-Address
selector. The focused correction requires a fresh matching API observation and
includes only its IPv4. Ledger/session ownership, service grants and pre-send
audit remain mandatory; ACK alone retains the lease and credit. Real UDP tests
cover selectors, ACK/NAK invariants, invalid/stale/absent observations, authority,
ownership and failed audit. The real CHR corrected method verified API absence
and committed Admin-Reset Stop in one 77.169 ms sample. This does not complete
#54's full normal-worker journey or target L009 acceptance. See
[the qualified evidence](Subscriber-CoA-Targets.md).


### Issue #54: real normal-runtime vertical path

Clean normal MikroDash at the #57 source (tree identical to verified develop
`7d6f8121`) passed five continuous verified renewals of the same real CHR session,
with every browser closed before renewal. Actual HTTPS portal/ticket/browser
POST returned to a connected dashboard in a strengthened 2.105-second sample.
Fast and Economy read back as verified exact-device ceilings; reconnect retained
account consumption and the same subscription/expiry. A bounded real transfer
and protected administrative suspension then verified persistent accounting and
background Disconnect/Stop in a 1271.552 ms sample. The fixture's missing RBAC
grant was corrected using existing grant persistence and a call-site regression.

These are CHR protocol results. Automatic HotSpot landing, further faults, gigaword enforcement, full
User Manager migration/coexistence and target
acceptance remain open. Measured one-client process resources and material write
volume are recorded in [the lab guide](Subscriber-CHR-Lab.md). Earlier local
ordinary-storage claims are qualified by the explicit Go 1.27 GOTMPDIR correction
and the complete disk-backed subscriber race rerun in
[verification](Subscriber-Verification.md). No operator capabilities were enabled.


Normal-runtime hard exhaustion subsequently passed on an explicit v2 synthetic
subscription while preserving account history. A 65536-byte download grant
stopped the real client; final accounting recorded 88124 bytes and an honest
22588-byte overrun, zero remaining and no duplicate API/RADIUS debit. The
exhausted user could access the portal but actual HotSpot re-entry was refused;
portal administration remained 404. Ordered CHR RADIUS selection proved that
primary rejection does not fall through to the secondary stand-in. Both normal
and injected-failure probes restored the owned configuration; the fault run
verified exact readback after its intentional exit 1. This is not actual User
Manager or live C routing acceptance. See the lab guide for commands and limits.


### Issue #59 delivery and final CHR fault replay

The missing-final accounting grant defect discovered during the real CHR outage
is delivered by PR #60 at `develop` `3bfdbd25`, with exact-head and post-merge
CI green. Known counters and holds remain intact, new admission and renewal are
blocked until final accounting, and unlimited zero-hold sessions have a review
path. Trusted final Stop and audited administrative recovery remain distinct
from period-byte allocation review. See [final accounting](Subscriber-Final-Accounting.md).

On clean application source `1abd51de`, the public CHR browser `reconciling`
case confirms actual HotSpot re-entry refusal with positive known credit and
continued portal access; no new active session or confirmed counter change is
accepted. This supplements the earlier short-restart and failed old-code outage
results without deleting them. The CHR harness full gate, disk-backed lab race
and ownership/evidence mutations are recorded in verification. #54 delivery and
all remaining AC-01..AC-19 software/target requirements remain open.


### Issue #62: trusted duration observation continuation

The shared observer now rejects malformed active uptime before completeness and
keeps invalid remaining timeout unknown. A real call-site regression shows the
old code closing a trusted active lease from garbage uptime; the correction
preserves the prior ledger. Explicit zero, counter direction and valid large
integer bytes remain separate. No new command/channel or legacy collector
parser change is required. Independent big.Int arithmetic random checks and
six compiling mutants pass their intended assertions; the clean native CHR
journey/rate/renewal path also passes on actual short-session wire values.
Full delivery and target L009 acceptance remain separate gates. See
[duration observations](Subscriber-Duration-Observations.md) for source, storage
and fuzz instrumentation limits. The wider AC-01..AC-19 gaps remain in scope.


### Issue #64: connection-entry backend checkpoint

The independent feature branch adds NAS/peer-bound one-use ticket metadata,
atomic authorization outcomes and an owned connection-status read. This is a
backend checkpoint: WebSocket connection events, the portal entry interaction,
HotSpot handoff files and complete real-client acceptance are still pending.
The interrupted RAM worktree was recovered into persistent storage after the
host restarted. Earlier focused tests do not replace the required fresh rerun;
no completion or live-C claim follows from this checkpoint.


### Issue #64: tested connection backend continuation

The coherent owned connection reader now distinguishes committed authorization
from fresh router confirmation and stale evidence. Declared connection socket
events recheck owner/peer before upgrade and session validity per send, using
the existing delivery loop and no per-browser NAS polling. Authenticated UDP
regressions retain transaction-time quota/network/device reasons and immutable
rejections; a new ticket recovers after an entitlement renewal. Audit fault
injection retains conservative grants and never sends Access-Accept before the
verdict commits. Disk-backed migration preserves v42 credentials, Unicode,
balances and active leases; v37 migration also passes.

These are focused automated results, not completed entry or target acceptance.
The English portal interaction, handoff artifacts, full real-client CHR journey,
full repository checks and tested develop delivery are still pending. See
[the backend contract](Subscriber-Connection-Entry.md). The local VS Code NAT
Shadow preparation and private reachability results do not enable a C pilot
or modify operator RADIUS/firewall settings.


### Issue #64: portal consumer continuation

The generated connection event is consumed by the actual isolated portal.
Separate portal and Internet states retain fresh exact-device evidence, scoped
denial reasons and bounded stale recovery. An explicit credential-submit may
request Internet for an enrolled landing hint; opening a URL or retaining a
cookie never does. Return references are strict, owned and optional in storage;
late sign-out responses do not revive the prior view. Ambiguous network keys
are refused before device lookup. All 149 frontend tests and the four-case
synthetic TLS/proxy browser journey pass, including keyboard and 375/1280/three
themes with zero findings under the existing axe tags. These remain Shadow
checks; the NAS handoff and complete CHR/target entry are still required.

### Issue #64 delivery and #66 red regression

PR [#65](https://github.com/so7ob/MikroDash/pull/65) delivered the scoped owned
connection entry at `develop` `06ae01a64707dda9c32d749f1884fe85f27ffb6a`.
[Current PR-head CI](https://github.com/so7ob/MikroDash/actions/runs/39518017811)
and [post-merge CI](https://github.com/so7ob/MikroDash/actions/runs/39518622305)
both passed. Issue #64 is closed; full-module #38 remains open. Automatic CHR
landing, one credential form, verified speeds, 19 browser-free renewals and
automatic exhausted/missing-Stop account recovery passed. Actual cap excess and
the preserved unresolved outage ledger are documented in the
[connection acceptance](Subscriber-Connection-Entry.md). No L009/C deployment
or production write followed.

Independent [#66](https://github.com/so7ob/MikroDash/issues/66) starts from that
verified base on `feat/66-calendar-boundary-renewal`, in the visible checkout and
on origin. Regression-first revision `078b37dc` adds ten intentionally red
virtual-clock ledger cases: admission and renewal each stop at the hourly
minute-25, daily, weekly, leap-month and short-month edges despite eligible
future entitlement. Failures are behavioral assertions, not build errors.
Bridging and future-period reservations are not implemented by that checkpoint;
its tests are not green and it is not a completed delivery. The larger policy,
administration/import/setup and target acceptance gaps remain active.

### Issue #66: calendar delegation implementation checkpoint

Admission and renewal now reserve every potentially touched hard-limit period
atomically while retaining current display evaluation. Initial relative timeout
start uncertainty is reserved across its accepted -5/+60-second interval; absolute
expiry/windows can conservatively refuse a new login near closure. Existing
unknown-start legacy grants remain reconciling. Known legacy exposure is retained
with an audited upgrade, including successful idempotent admission retries.
The new regression first failed because that retry returned before transaction
commit; the correction preserves future holds and exactly-once audit.

Durable combined renewal includes required future throttle rate, timeout and
cumulative byte caps. A real loopback authenticated UDP case rejects rate-capability
revocation and requires exact rate readback beyond ACK/timeout/byte confirmation.
Prepared operation replay also rechecks account suspension before a network send;
the previous behavior sent a renewal after suspension and is retained as red evidence.

Five-anchor ledger rollback and concurrent cross-network admission/renewal tests
pass. The package race run passes (82.738 seconds); the 30-second independent
calendar-conservation fuzz run passed 77040 cases. Full repository checks,
mutation evidence, actual CHR calendar crossing and protected develop delivery
remain pending. These results do not complete AC-03/09/10 or the wider module.
See [the bounded calendar contract](Subscriber-Calendar-Delegation.md).

Separately, the owner approved narrow C portal reachability and existing-router-CA
signing. Actual C-device HTTPS opens without warnings after owner F5 restart; the
live leaf and isolated routes were verified. No existing HotSpot certificate,
RADIUS authority or production connection was changed. This narrows the portal
TLS/reachability gap in AC-13/14, but Internet admission is still disabled in Shadow
and those target acceptance rows remain pending.

### Issue #66: real reset and full verification

Exact implementation `82f52e99` passes native Go 1.27.3 format, vet, all 64 tested
packages, current vulnerability scanning, generated code/tables, TypeScript,
frontend build, all 150 frontend tests and all 32 tooling tests. The prior
Go 1.27.2 scan failed on the reachable standard-library GO-2026-6809 advisory;
that failure is retained and resolved with the official checksum-verified patch,
not a skipped check. The existing owner toolchain/debugger was not replaced.

The exact tracked source was byte-matched against its verification archive;
all six compiling mutants were killed by behavior and the archive was restored.
The newly expanded authenticated UDP calendar scenario also passes -race for all
five anchors (4.054 s). Actual CHR 7.24.5 with the normal background worker and an
actual namespace HTTPS client retained the same session through an hourly reset
and five verified renewals. Its203-second live sample split 90/113 across the edge;
final trusted accounting committed 204 seconds with no pending finality. A 64 KiB
post-reset payload was accounted only to that period. The original unresolved
account, balances and subscription were preserved. No boundary-gap byte split
or target resource/latency bound is claimed.

The first direct-portal client attempt correctly failed missing HotSpot host
presence before any lease. The corrected fixture visited HotSpot first and used
an explicitly audited immutable reschedule rather than changing old usage. The
final documentation and five-case UDP expansion still require current PR-head
and post-merge CI. PR #67 remains the scoped delivery; #38 and all outstanding
module/target requirements remain open.


### Same-profile pilot routing: Issue #70

Implemented guarded disposable-CHR domain selection and injected-failure probes.
Actual 7.24.5 measurements cover 12 HTTPS attempts plus a positive control. Only
an endpoint placed first with `split-user-domain=yes` received qualified pilot
names exclusively, normalized to the existing account username. Plain names
remained with the primary. Every attempt was rejected; exact original non-secret
configuration and profile fields were restored in normal and deliberate-failure
runs. Call-site tests cover ambiguous writes, evidence failure and drift refusal.

This advances the routing dependency of AC-13/14. Production domain handoff,
legacy name conflict detection, accounting/CoA identity and operator C acceptance
remain pending. No target configuration changed and no AC row is closed by a
reject-only lab measurement. See Subscriber-CHR-Lab.md for the measured matrix.
