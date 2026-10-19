# Subscriber Management development preview

**Issue #38 is not acceptance-complete. Do not cut production authentication over to this branch.**
This document separates working code from outstanding integration. The feature is disabled
unless `-subscriber-config` names an explicit configuration file. Existing admin accounts,
RouterOS users, settings encryption, GeoIP and the patched RouterOS library are retained.

## Administration navigation

Open **Subscriber Management** from the main sidebar beside Dashboard, or visit
`/subscribers`. The section contains Subscribers (including voucher creation and plan
assignment), Plans and policies, and Policy simulator. The page retains the existing
`subscribers` permission key and the backend `system:subscribers` authorization check.
The subscriber portal remains on its isolated listener.

## Ownership and architecture

MikroDash owns subscriber credentials, immutable plan versions, subscriptions, period usage,
reservations and the accounting journal in the existing `mikrodash.db`. Schema migrations 38 and 39
add `subscriber_*` tables; it does not rewrite `users.json` or use User Manager as a hidden
runtime backend. User Manager is currently a read-only preview source.

`internal/subscriber/policy.go` is a pure policy evaluator. `storage.go` and `accounting.go`
implement SQL transactions. `radius.go` uses pinned `layeh.com/radius`, including its packet
parser, PAP support, keyed accounting authenticators and MikroTik vendor dictionary. An
additional standard HMAC-MD5 Message-Authenticator check is mandatory on Access-Request;
responses are signed. This avoids a FreeRADIUS sidecar and a second authoritative database.
MPL-2.0 attribution is recorded in the dependency catalogue and third-party notices.

`runtime.go` runs enforcement without browser watchers. `internal/server/subscribers.go`
retains the existing router session with a collector-free `subscribers` hold. Device checks
use that shared connection and its concurrency limiter. Disconnect uses authenticated RADIUS
with a narrowly scoped `service:subscriber` grant through `internal/guard/subscriberguard.go`.
A fresh API check must still match account, network, device and connection start time before
a disconnect is sent. An ACK is recorded as acknowledged, never as verified disappearance.

## Data and policy contracts

- A subscription references one immutable `(plan ID, version)`. Renewal creates a new ID and
  supersedes the previous subscription, with an audit reason. Renewal currently refuses
  outstanding sessions until they are reconciled.
- Password accounts use the existing scrypt implementation. A voucher is a random 16–64
  hexadecimal character secret, stored as a SHA-256 digest, with an independent username.
  Voucher plaintext is returned once when created. Names and other Unicode content are retained.
- All byte, rate and duration quantities are signed 64-bit non-negative values, encoded as
  **decimal strings in JSON**. Rates are bit/s, traffic is bytes, and time is integer seconds.
  Overflow is refused; frontend formatting uses `BigInt`.
- Policies contain independent limits. Time, download, upload and total can all constrain one
  subscription, each with a renewal period and action. Calendar hourly, daily, weekly and
  monthly periods use explicit IANA zones and anchors. Anchors on days 29, 30 and 31 clamp to the last day of shorter months without drifting later months.
  An hourly anchor at `00:25:00` resets at minute 25. A disabled reset is a non-renewing period.
- Access windows use the weekday of their opening edge and support crossing midnight. Their
  end is exclusive: an inclusive source time of `22:59:59` must map to `23:00:00`.
- `limited`, `unlimited`, `unknown` and `disabled` are explicit states. A limited zero is
  exhausted. Import preview retains raw RouterOS zero values without silently converting them.
- Time is the **sum of device connection seconds**, not union wall-clock time. One outstanding
  lease per device is admitted. The session count is shared across the account's networks.
- Rate ceilings are per device. Selection intersects the plan ceiling and active throttle
  ceilings. The present UI offers plan-entitled profiles only. Speed selection cannot change
  a subscription ID, expiry, journal row or usage bucket.

`Remaining` in the admission decision is spendable capacity after outstanding reservations.
The portal separately shows total remaining entitlement and reserved capacity. Unknown storage
or policy state denies new network admission. Portal credential verification remains available
for suspended or expired accounts.

## Accounting and outage behavior

Each authorization gets a random durable lease ID in the RADIUS Class attribute. Accounting
must echo it and match NAS source, network, calling station and Acct-Session-Id. The lease
therefore separates reused RouterOS session identifiers and delayed Stop packets.

An immediate SQLite transaction inserts the deduplication key and updates independent cumulative
high-water marks. Only increases are charged. Lower or duplicate snapshots never subtract
previously committed usage. Accounting-Response is sent only after commit; storage failure
receives no acknowledgement. Production startup requires SQLite synchronous FULL.

RADIUS input octets are subscriber upload; output octets are subscriber download. Gigawords
are combined before checked conversion to signed 64-bit storage. Version/evidence-bound API observations enter the same cumulative high-water transaction as RADIUS, so overlapping readings are not charged a second time. API octet direction requires a recorded HotSpot traffic probe; an unknown or different router version cannot renew a session. A one-second snapshot skew does not invalidate the account.

Connection seconds are split at period boundaries. Bytes inside an unsampled boundary gap
are charged conservatively to every touched period and labelled `boundary-conservative`;
account lifetime bytes are counted only once. This is an upper bound, not a claim of measured
per-period distribution. There is no automatic refund/reconciliation implementation yet.

Admission reserves sub-budgets atomically across all outstanding sessions. It emits local
Session-Timeout, directional MikroTik byte limits and total byte limits where configured.
The current implementation limits every delegation to **60 seconds**, the nearest hard period
or access-window edge, or its smaller time allocation. A shared background observer and durable CoA operations can renew this bounded delegation on a NAS with explicitly recorded timeout, byte-limit and counter semantics. Five continuous renewals pass in the UDP lab; target-router proof remains pending. Renewal across a hard calendar-period edge remains outstanding. It never grants a
new full hour after partial consumption.

A 2-second background loop requests disconnect on expired delegations, suspension and
applicable exhausted disconnect policies. Requests have a 3-second transport deadline and
10-second retry spacing. These are configured timings, **not measured router response bounds**.
A missing Stop retains its reservation until a complete shared API read proves absence after the maximum attempted local delegation. The session slot then closes, while unresolved quota remains in explicit holds rather than becoming measured usage or free credit. A covering final Stop resolves those holds. Administrative resolution and boundary-byte settlement remain outstanding.

`deny` affects admission; `disconnect` also triggers enforcement. `alert` produces decision
reasons only (notification delivery is not implemented). `throttle` also changes active session rates through guarded CoA when its capability is recorded. Durable operations distinguish requested, applying, acknowledged, verified, failed and pending-reconnect. Only a fresh exact-device queue readback verifies a live rate; ACK alone is insufficient. Unknown capabilities retain pending status. Burst/minimum/priority and network ceilings remain outstanding.

## Portal boundary and authentication

The dedicated TLS listener serves only the subscriber document, bundle, selected local assets,
owned account routes and its own WebSocket hub. It has no administration fallback. Cookies
use a separate `__Host-` name, Secure, HttpOnly and SameSite=Lax. Origin and CSRF token checks
protect mutations; WebSockets recheck portal session validity at each send. Portal and RADIUS
share a two-operation credential-verification bound, so an allowed rate-limit burst cannot
start unbounded concurrent scrypt allocations. A CSP restricts
resources and HotSpot form destinations. The existing configured trusted proxy chain controls
client IP attribution; never trust arbitrary browser MAC/IP fields.

Portal login establishes only a portal session. `POST /api/connect` reads the client's device
from the enrolled router's HotSpot host table, then creates a 30-second network/device-bound
one-use ticket. The browser POSTs it to the configured HTTPS RouterOS `/login` URL, using the
account's actual username. The owned RADIUS handler consumes the ticket; an exact packet retry
is idempotent and a different packet cannot reuse it. RouterOS `dst` is the configured portal
origin, not a browser-provided return URL. This browser-to-router leg still needs live acceptance.

The implemented password protocol is PAP over the HTTPS HotSpot login leg. CHAP is refused;
a one-way scrypt hash cannot provide the original password CHAP needs. Existing HotSpot
login methods are never changed by the application. Portal logout does not disconnect devices.
A device disconnect requires explicit confirmation and server-side ownership checks.

## API and events

Administrative routes require authenticated modern admin sessions and `system:subscribers`.
This global permission is structurally limited to global administrator grants. POSTs require
same-origin `Origin` and `X-MikroDash-Request: subscriber-admin`.

| Route | Contract |
| --- | --- |
| `GET /api/subscribers` | Accounts, immutable plans and runtime mode |
| `POST /api/subscribers` | `create`, `plan`, `assign`, or `status`; each requires a reason |
| `GET /api/subscribers/snapshot?account=...` | Read-only policy evaluation and current ledger snapshot |
| Portal `POST /api/login` | Username/password, or blank username with voucher |
| Portal `GET /api/session` | CSRF token, enrolled network keys and mode |
| Portal `GET /api/account` | Session-owned versioned snapshot; account query parameters confer no access |
| Portal `POST /api/speed` | Entitled `profileId`, 10-second cooldown; returns the persisted requested/pending-reconnect state |
| Portal `POST /api/connect` | Enrolled `network`; returns one-use credentials and allowlisted destinations |
| Portal `POST /api/disconnect` | Owned `leaseId` and `confirm: true` |
| Portal `POST /api/logout` | Revokes only the portal session |
| Portal `GET /ws` | `subscriber:snapshot`, generated TypeScript payload contract |

Snapshots include a content-derived version, `observedAt`, `lastSyncedAt`, quality, separate
lifetime counters, subscription, per-limit decisions and recent sessions. The portal delivers
ledger snapshots every two seconds; this **does not mean the router was measured every two
seconds**. Standard RADIUS interim is 60 seconds, not 5. The display's interpolated advance is
bounded and labelled. The retained shared router connection now reads active sessions and dynamic queue ceilings every worker cycle, with explicit version/counter evidence and separate API observation/RADIUS synchronization times. Freshness and resource measurements, source-gap settlement and the target 5-second acceptance are still pending.

## Hardware facts and acceptance boundary

Read-only inspection on 2026-10-19 found RouterOS 7.24.5 (stable), L009UiGS-2HaxD,
Asia/Aden, three HotSpot servers and C using HTTPS, RADIUS accounting and its local 5s interim
setting. Incoming RADIUS is enabled on 3799 and the application bridge still exists. No active
FastTrack rule was found in that snapshot. This does not prove forwarding/IPv6 isolation.
The configured router-container application address was not independently verified. Private
addressing and raw account/topology reports are deliberately excluded from public documentation. User Manager currently exposes one profile, one limitation,
one profile link, two users and two user-profile rows. The limitation is one hour with daily
reset. Historical definitions mentioned in the request are not assumed to exist today.

No router write, production disconnect, deployment, RADIUS cutover or certificate replacement
was performed. The existing logo asset is MikroDash-branded; a separate So7ob logo asset was
not found and none was generated.

## Outstanding acceptance work

This is a development preview, not the complete requested module. Outstanding items include:

- Verified live C single sign-in, packet accounting directions on actual traffic, local byte
  cap enforcement, reboot/Stop-loss reconciliation, bounded delegation renewal, and exact
  CoA Session-Timeout semantics.
- Target-router verification of implemented CoA rate/readback and operation recovery, plus speed network
  ceilings; burst/minimum/priority support and distinct long-lived device limits.
- Shared rapid API observations and reconciliation for the 5-second freshness target; true
  measured latency/CPU/memory/flash/channel budgets under load.
- Complete plan/schedule/speed editors, account edits, safe bulk operations, audit/usage screens,
  scenario simulation, complete device history and setup wizard.
- User Manager mapping/apply and approved period opening balances. The CLI implements preview
  only and cannot silently import lifetime totals into period balances.
- Target verification of implemented same-NAS HotSpot network mapping and days 29/30/31; expiry
  activation grace semantics, boundary-byte reconciliation,
  and notification delivery for alert-only limits.

## Official references

- [RouterOS RADIUS](https://manual.mikrotik.com/docs/authentication-authorization-accounting/radius/)
- [User Manager](https://help.mikrotik.com/docs/spaces/ROS/pages/2555940/User%20Manager): multiple
  profile limitations are supported; this implementation does not assume otherwise.
- [HotSpot](https://manual.mikrotik.com/docs/authentication-authorization-accounting/hotspot-captive-portal/)
- [RFC 2866](https://www.rfc-editor.org/rfc/rfc2866),
  [RFC 2869](https://www.rfc-editor.org/rfc/rfc2869),
  [RFC 5176](https://www.rfc-editor.org/rfc/rfc5176)

The current manual identifies itself as 7.26. Read-only menu results above came from 7.24.5;
newer documentation is not hardware proof.
