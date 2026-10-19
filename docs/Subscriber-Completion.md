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
| AC-03 | Five continuous renewals, crash after application | Durable total-session CoA renewal implemented; five UDP/virtual-clock cycles and post-NAS/pre-ACK crash recovery pass. Relative-mode safety and hard-period continuity remain code gaps | operations, reservations, verified timeout semantics | partial | passed: scoped UDP/crash tests | pending: scoped timeout probe |
| AC-04 | Concurrent daily/monthly time/byte limits | Pure/ledger tests; live exhaustion pending | policy/accounting/runtime | partial | prior pass | pending: traffic test |
| AC-05 | Live rate switch/throttle, correct device | Real UDP rate switch and active throttle with exact-device queue readback pass; burst/minimum/priority/network ceilings pending | persistent CoA operations, queue readback, capabilities | partial | passed: UDP + queue readback | pending: rate probe |
| AC-06 | Reordered/retried accounting, reused IDs | High-water journal tests; more retransmission faults needed | radius/accounting | partial | prior pass | pending: retransmission capture |
| AC-07 | API/RADIUS unified debit, one-second skew | Shared capability-bound API observations use the cumulative ledger; overlapping older RADIUS sample regression passes | observation matching and accounting | partial | passed: shared ledger regression | pending: direction proof |
| AC-08 | Missing Stop/restarts reconciled safely | Complete API absence closes the seat after issued bounds; unresolved exposure remains a quota hold until covering Stop. Audited admin settlement and explicit byte-gap allocations implemented; incomplete telemetry review remains pending | absence evidence and administrative settlement | partial | passed: absence/Stop holds | pending: restart probe |
| AC-09 | Calendar anchors and byte-gap settlement | Days 29/30/31 clamp without drift, leap tests pass; approved byte-gap settlement implemented; continuous hard-edge renewal pending | policy, journal adjustments | partial | passed: calendar anchors and approved settlement | pending: boundary observations |
| AC-10 | Shared quota and same-NAS networks | Atomic admission tested; same-source networks map by authenticated Called-Station-Id/Class; UDP forgery regression passes | radius network mapping, renewal locking | partial | partial | pending: multi-device C/D lab |
| AC-11 | No browsers required | Background Disconnect UDP test | explicit runtime lifecycle and observer | partial | partial | pending: closed-browser run |
| AC-12 | Local outage bounds and fail-closed admission | Initial 60-second/byte delegation | verified renewal and local limits | partial | partial | pending: service outage probe |
| AC-13 | Single C sign-in and safe exhausted portal | Isolated portal/ticket server tests | actual client HTTPS POST and verified session | partial | partial | pending: client behind C |
| AC-14 | Ownership/device/network/ticket isolation | Existing portal and RADIUS tests | expanded runtime/operations security | partial | prior pass | pending: C isolation |
| AC-15 | Authenticators, commit-before-reply, faults | UDP forgery/storage failure tests; incomplete counter-payload validation remains a software gap | crash/rollback and operation replay tests | partial | partial | pending: target protocol |
| AC-16 | Complete admin/portal/browser workflows | Four tabs including tested reconciliation; full editors/bulk/history/setup still pending | backend edit/search/bulk/history/simulator/setup, UI | partial | partial | pending: final browser journey |
| AC-17 | Idempotent import/opening/cutover/rollback | Read-only User Manager preview | transactional mapping/apply/approval | partial | missing | pending: approved live batch |
| AC-18 | Measured freshness/CoA/disconnect/resources | Duplicate transaction benchmark only | shared observer and lab load harness | partial | missing | pending: target load approval |
| AC-19 | Target build and existing app compatibility | Verified foundation/session develop CI and static ARM64 cross-build; target execution pending | build/regression gates and isolated data | partial | prior pass | pending: target build |

## Capability matrix

| Capability | Official reference | Implementation baseline | Lab | Target |
| --- | --- | --- | --- | --- |
| RADIUS PAP/Class/accounting | RouterOS RADIUS; RFC 2866 | implemented | UDP tests passed | pending |
| Session-Timeout at admission | RouterOS RADIUS | implemented, max 60 seconds | attributes tested | enforcement pending |
| Session-Timeout CoA total/remaining meaning | RouterOS lists writable attribute; meaning needs probe | durable total-session implementation | five-cycle UDP pass | pending |
| CoA rate change | RouterOS RADIUS Change of Authorization | durable rate operations and capability gate | synthetic UDP/readback passed | pending |
| Dynamic queue identity/rate verification | RouterOS queue and HotSpot active menus | exact unique dynamic host queue parser | parser + rate readback passed | pending |
| API HotSpot octet direction | Conflicting service-specific wording requires one-way traffic | explicit per-source capability mapping | synthetic direction/overlap passed | one-way probe pending |
| Shared NAS source distinguished by HotSpot server | Called-Station-Id documented as HotSpot server | authenticated Called-Station-Id and owned Class mapping | UDP isolation passed | pending |
| Interim >=60 seconds | RFC 2869 section 5.16 | implemented | attribute test passed | local 5s compatibility unverified now |

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
Test databases and journals continue to use the ordinary filesystem.

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
  full verification's SQLite tests used the ordinary filesystem. Neither proves
  media durability under power loss or execution on the target router.
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
separate business balance versus new-session availability in the portal, and large
pending-operation queue recovery. These are part of the original contract, not
hardware-only acceptance exceptions. No completed full-module claim is made.
