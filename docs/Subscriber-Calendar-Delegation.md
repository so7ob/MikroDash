# Bounded authorization across quota resets

Issue #66 extends admission and durable renewal without changing the accounting
source of truth. This is an implementation contract; target-router calendar
acceptance and complete Subscriber Management acceptance remain pending.

## Authority and period accounting

The existing SQLite ledger owns confirmed usage, provisional byte-gap exposure,
reservations and operation intent. A subscription still references one immutable
plan version. An hourly, daily, weekly or monthly reset creates another period;
it does not clear an earlier bucket, lifetime usage or subscription identity.

The pure planner intersects every active hard limit. Each crossed period gets an
independent reservation in the same immediate transaction as the new lease or
renewal operation. Other sessions' reservations remain unavailable. A renewal
subtracts its own reservation once before planning, then retains the maximum of
its previous exposure and new allocation. A smaller response does not establish
that an older attempted packet can no longer arrive.

Connection time is the sum of device seconds. Its reservation is the possible
overlap with each period, and confirmed seconds are split at the actual trusted
session start and counter interval. Bytes cannot be distributed within a missing
boundary sample: the same local cap is reserved in every potentially touched
period, including a sample exactly at the reset endpoint. Lifetime bytes still
increase once. Boundary-gap provisional holds and reasoned allocations retain
the [existing reconciliation contract](Subscriber-Reconciliation.md).

## Admission and uncertain session start

`Session-Timeout` at admission is a relative NAS duration. The existing accounting
ingress accepts a first session start from five seconds before the authorization
through sixty seconds after it. An immediate NAS start must not be assumed when
reserving a grant that can cross a reset.

Admission reserves the worst overlap independently for every accepted start in
that interval. A local delegation remains between one and sixty seconds. Until a
trusted first reading supplies the start, its local expiry can therefore be up
to 120 seconds after reservation. The stored pending deadline reflects that
exposure. A trusted start anchors the deadline to start plus issued duration.
This is not a promise that an unconfirmed pending session expires sixty seconds
after the application's request.

A hard subscription expiry or access-window closure is an absolute cutoff. The
relative grant is shortened to end before that cutoff even at the latest accepted
start. If no positive duration can satisfy it, admission is refused with
`initial-timing-budget-unavailable`. This conservative refusal can affect a new
login in the final sixty seconds before closure. No verified absolute termination
attribute is claimed. Access is not extended to conceal this limitation.

Every possible byte endpoint must also have trusted positive capacity. Unknown or
exhausted future periods shorten the grant before that endpoint or refuse it.
Current-period display evaluation retains its existing reset edge; it does not
present a future period's reserved balance as consumption in the current one.

## Renewal, rate intersection and verification

A renewal starts reservation coverage at the trusted NAS counter high-water mark,
including elapsed time that the latest API observation has not yet reported. Its
new local deadline is at most sixty seconds from that mark, also bounded by hard
expiry, schedules and available period budgets. Observation completion latency
does not grant additional time. The persisted time basis remains
`nas-uptime-v1`; timeout and byte encoding still require recorded total-session
NAS semantics. An unverified or relative-only implementation is refused.

The selected speed, plan ceiling and throttle policy in every touched period
are intersected before persisting the grant. If renewal needs a lower rate, or
the exact-device rate is unknown/higher, the durable request sets `requireRate`.
The same CoA carries that rate with the reserved timeout and cumulative byte caps.
It requires recorded live-rate capability. Exact queue-rate readback is part of
verification; timeout/byte readback and CoA-ACK alone cannot verify it.

Before replay, the current account status, pending accounting, immutable
subscription, selected speed, period policy, NAS capabilities and owned observed
session are rechecked. Recording an already-issued verified fact is separate from
authorizing a new packet. Suspension after operation preparation prevents a new
renewal send. Failed audit or commit cannot leave only part of a new reservation
or operation intent in durable storage.

## Existing leases and recovery

An old reserved lease without the start-window basis remains reconciling until
trusted accounting or review resolves its unknown exposure. A known active legacy
lease retains the maximum initial/attempted timeout and byte caps across all
possibly touched periods before another device is admitted or renewed. This
upgrade is audited and atomic. An idempotent admission retry must commit that
upgrade before returning the same issued grant; it never creates a second lease
or charges confirmed usage. Invalid legacy context requires review.

No schema replacement is needed: additive metadata lives in existing durable JSON
requests, and period allocations use the existing reservation key. Old pending
renewals retain the [legacy replay fence](Subscriber-Management.md#uptime-based-renewal-budgets).
Existing credentials, data encryption and historical accounting are preserved.

## Verification boundaries

Virtual-clock SQLite cases cover minute-25, Aden midnight, weekly, leap-month and
short-month resets. Tests also cover every accepted start offset, unavailable byte
endpoints, hard schedule/expiry cutoffs, audit rollback, concurrent admission on
another network, and the legacy retry upgrade. Loopback authenticated RADIUS UDP
cases for all five anchors assert the combined timeout/rate/byte attributes and rejects verification
until the exact rate is observed. These are synthetic protocol/ledger checks.

A 30-second interval-conservation fuzz run completed 77040 cases without failure.
It independently intersects each accepted start interval with the calendar periods,
checks time and byte-endpoint coverage, and bounds reservations by spendable shared
capacity. This result does not establish flash durability, performance under load
or actual NAS acceptance. The package race check passed in 82.738 seconds, and the expanded five-case UDP
race run passed in 4.054 seconds. Six compiling behavior mutants were killed.
Full native Go 1.27.3 format/vet/tests, live vulnerability scanning, frontend build,
generator checks, 150 frontend tests and 32 tooling tests passed. The earlier
Go 1.27.2 vulnerability failure is retained; its standard-library advisory was
resolved by the official patched toolchain, without suppressing the check.

On exact implementation `82f52e99`, the normal MikroDash worker and an actual
namespace HTTPS client on isolated CHR 7.24.5 retained one session through a real
hourly reset and five readback-verified renewals. At the final live sample, 203
seconds split into 90 old-period and 113 new-period seconds. Actual logout/final
accounting committed 204 seconds with no pending finality. A 65536-byte payload
delivered after the reset produced 66055 download and 1924 upload counter bytes;
the download bucket belonged only to the new period. TCP return traffic and
network overhead are not application payload. This did not measure byte
distribution inside a boundary gap.

The first client attempt reached the portal directly and correctly failed device
presence verification before any lease was issued. The corrected lab journey
visited the actual HotSpot landing first; an explicit immutable v2 reschedule
retained the refused unissued fixture and earlier unresolved account unchanged.
No operator RADIUS or Internet admission setting was changed. Exact PR-head and
post-merge delivery evidence is maintained on PR #67; target C calendar acceptance
remains pending. See [verification](Subscriber-Verification.md#issue-66-calendar-delegation-verification).
