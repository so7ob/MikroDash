# Subscriber connection entry: implementation checkpoint

Issue [#64](https://github.com/so7ob/MikroDash/issues/64) continues the existing
HTTPS portal/PAP ticket path. The backend and English/LTR portal now consume the
owned connection contract. This is not completed single sign-in or target-router
acceptance: reviewable HotSpot handoff rendering and the owned-CHR installer are implemented;
the full browser-originated CHR journey remains pending. Do not enable production authentication or capability flags
based on these tests.

## Ownership and durable decisions

An authenticated portal session may request a ticket only for an entitled
enrolled network. An ambiguous network key matching multiple NAS entries is
refused before ticket issuance; the last entry is not chosen. The backend resolves the device from that NAS HotSpot host
read using the trusted HTTP peer; browser MAC/IP claims grant nothing.

The ticket request now records the NAS router ID, network, verified device,
trusted peer and account in the existing SQLite authority. Only a SHA-256 ticket
digest is retained as the connection reference. The 30-second secret is returned
once for the browser HTTPS POST; no password or ticket enters browser storage or
public assets. A reference alone cannot authorize Internet or read a flow.

Redeeming requires the authenticated NAS router, network, device and actual
username to match before consumption. The same RADIUS request may repeat;
another NAS or request cannot consume or reuse the ticket. A verdict records the
exact bounded lease/request identity, not another account session. The positive
verdict and its audit commit before Access-Accept may be sent. Failed audit or
storage keeps the conservative reservation and sends no Access-Accept. An exact
retry can recover the same reservation after a transient verdict failure.

Reserve supplies transaction-time denial reasons, including exhausted/unknown
limit IDs, network entitlement, session/device conflicts and reconciliation
requirements. Unexpected database failures become authorization-unavailable,
not guessed quota failures. A committed rejection is immutable under exact
retransmission, including after an administrator renews entitlement; the client
must request a new ticket. Outcome retries never create another audit or lease.

Migration 43 adds only `subscriber_connection_flows`, with foreign keys to the
existing ticket and lease. It does not rewrite credentials, subscriptions,
usage, reservations or active sessions. Legacy short-lived tickets without NAS
and peer metadata cannot redeem; request a new ticket. Existing active leases
continue accounting. No old database is restored for rollback.

## Owned read and event contracts

`POST /api/connect` also returns `flowId` and a server-built destination:
`<configured HTTPS portal origin>/?connection=<digest>`. The destination accepts
no browser return URL. The reference is not an Internet or portal credential.

`GET /api/connection?flow=<digest>` requires the subscriber cookie, the same
account and trusted peer. Missing/foreign/malformed references return a generic
404; an anonymous request returns 401. The read is a single coherent SQLite
query and performs no browser-specific RouterOS request.

The existing isolated `GET /ws` supports optional `?flow=<digest>`. It rejects
unowned context before upgrade and rechecks the portal session before each
frame. It continues `subscriber:snapshot` and sends declared, generated
`subscriber:connection` frames when the owned connection version changes. Both
reuse the existing two-second delivery loop and shared background observations.
Subscribers cannot join administration rooms or send administration events.
Disconnecting browsers does not stop accounting or enforcement.

The connection payload contains `version`, `flowId`, `stage`, `network`, `device`,
`leaseId`, non-null `reasons`, `requestedAt` and `observedAt`. Times are UTC RFC3339.
The content-derived version changes with a state or evidence change; it does not
turn the rendering frequency into a router observation frequency.

| Stage | Evidence |
| --- | --- |
| requested | Ticket issued; no network authorization yet. |
| authorizing | The bound NAS request redeemed the credential; no committed positive verdict yet. |
| authorized | A bounded lease is committed; this device is awaiting router confirmation. |
| connected | Fresh exact-device/NAS/server/start/address observation matches the linked lease and recorded router version. |
| unconfirmed | Observation missing after the initial five seconds, stale, future-dated or mismatched. Known account balances are retained. |
| rejected | Immutable local authentication/admission refusal with retained reasons. |
| expired | An uncompleted ticket passed its 30-second expiry. |
| disconnected | The exact linked lease ended; a newer session does not revive this flow. |

Start or Access-Accept alone does not establish connected. Another device in the
same account cannot confirm this requesting device. Observations older than
five seconds or more than one second in the future are unconfirmed. The original
trusted observation timestamp is preserved. Ticket expiry does not expire an
already authorized and independently confirmed Internet session.

For NAT/proxy deployment, verify the actual NAS and HTTP source paths first.
Never compensate for translated sources by trusting browser headers. The
existing explicit trusted-proxy boundary remains the only proxy authority.

## Verification at this checkpoint

The new regressions run against ordinary disk-backed SQLite, real authenticated
loopback UDP RADIUS, and real TLS/WebSocket connections. Router observations in
these tests are synthetic; this is not an L009/C or captive-browser result.

- Migration preserves the password, Unicode name, subscription/usage and active
  lease from v42, refuses legacy unbound tickets, and repeats idempotently. The
  existing v37 compatibility test also passes with the new metadata table.
- The lifecycle test distinguishes authorization from fresh confirmation, stale
  and future readings, a different router version, ticket expiry and final Stop.
- A negative-control device is positively observed connected while the owned
  requesting flow remains requested. Foreign peers/accounts cannot obtain it.
- Injected audit failures roll back ticket/flow issuance together and prevent a
  positive response. Exact-request recovery reuses the retained grant and audit.
- UDP denial cases cover exhausted and unknown time, network entitlement,
  concurrent session and duplicate device conflicts. Replays preserve balances,
  expiry and subscription; a newly entitled ticket can recover after renewal.
- TLS sockets reject foreign owners, peers, origins and invalid references before
  upgrade, deliver owned transitions, and stop delivery after session revocation.
- The declared-event no-null gate and portal administration/CSRF guards pass.

Focused commands compile test executables with the normal project toolchain and
then run them with both `GOTMPDIR=/tmp` and `TMPDIR=/tmp`. Compilation caches may
be in RAM; test databases are on the ordinary filesystem. Private red/green
logs remain under `.temp/subscriber-64-recovery` in the visible workspace.
The frontend type check and all 149 frontend tests pass. The real synthetic
Chromium journey passes in four native-TLS/proxy and normal/pending-accounting
cases, at 375/1280 in three themes, with restricted session storage and keyboard
checks. It verifies no admission from a GET hint or unowned return and zero axe
findings under the existing tags. It is Shadow, not actual HotSpot admission.
Full regression found two stale test-file counts after the new test was added;
the documents were updated to the measured 113 files without weakening the gate.
Final full regression, mutation/race checks, generated handoff artifacts and the
complete real-client journey remain required before delivery.

## Work still required for this issue

- Complete safe HotSpot handoff artifacts and prove explicit sign-in-and-connect
  and owned return handling through the real NAS, without repeat submissions or
  redirect loops. A GET hint or existing cookie never initiates admission.
- Test the complete browser-originated HotSpot path on the isolated CHR, including
  post-exhaustion access and a failed admission returning to the account.
- Finish repository-wide verification and deliver through the tested PR/develop
  workflow. The larger AC-01..AC-19 scope and live C approval remain separate.

## Reviewed HotSpot handoff artifacts

Build the frontend, then render an explicit enrolled network into a new directory:

```sh
go run ./cmd/webbuild -dir web
go run ./cmd/subscriberlanding -subscriber-config /private/subscriber.json \
  -network C -web web/dist -out /private/review/hotspot-C
```

The command is offline. It neither decrypts secrets nor connects to RouterOS.
It writes ten English/LTR servlet pages, the small classic JavaScript helper,
`api.json`, and a sorted SHA-256/size manifest. Each public asset is limited to
4096 bytes for bounded readback. Identical output repeats without rewriting;
changed or unknown files and symlinks are refused. Keep the full private runtime
configuration outside the public directory. Only the reviewed public assets are
intended for the NAS; the manifest is review metadata.

The helper accepts only the exact configured HTTPS portal root. A successful
return additionally requires one well-formed connection reference. Escaped
RouterOS return variables are decoded once; arbitrary paths, origins, credentials,
fragments, duplicate parameters and doubly encoded destinations are refused.
Login links carry only the encoded network hint. An existing portal cookie or
visiting that hint does not initiate Internet admission. Failure pages retain
an owned reference when available and return to the account without retrying.
A visible Continue to subscriber portal link remains available without JavaScript.
No raw RouterOS error, browser device claim, password or RADIUS secret is rendered.
Fonts and shared styles load from the local portal origin; only licensed public
font responses allow cross-origin loading, never account or administration APIs.

The owned lab tool provides `install-landing` and `rollback-landing` actions.
Both retain its existing per-command QEMU/NAS/topology guards. Installation
requires the idle exact `hs-lab`/`hsprof-lab`/`ether2` HTTPS fixture. It records
the original override privately, creates a manifest-derived directory, uploads
only absent files, reads each back before changing the profile, and verifies the
final override. A partial upload can resume only with the same review. Rollback
restores only the recorded override, refuses drift and preserves files/evidence.
These actions cannot target an enrolled operator router or read operator `/data`.

The isolated CHR 7.24.5 accepted the documented `/file/add` operations, returned
matching contents and served the rendered header policy. Repeated installation
and repeated rollback passed. Full browser admission/return acceptance remains
pending; these artifact checks do not establish C or L009 readiness.

References: [HotSpot customisation](https://manual.mikrotik.com/docs/authentication-authorization-accounting/hotspot-captive-portal/hotspot-customisation/)
and [RouterOS file operations](https://help.mikrotik.com/docs/spaces/ROS/pages/2555971/Files).

## Normal-runtime CHR browser checkpoint

The isolated CHR 7.24.5 journey now passes on application source `58452ee3`.
It starts at the actual NAS login page, follows the rendered landing, submits
one portal credential form and one actual browser HTTPS HotSpot POST, then
returns to the account with a fresh owned connection. The measured return to
`Internet connected` was 2250 ms in this single local sample; this is not a
p95, a guarantee or an L009/C measurement.

The browser had session storage blocked. Both Economy and Fast became verified
against the same device/lease and actual queue ceilings. Subscription identity,
expiry and byte totals remained unchanged; time continued accumulating. All six
375/1280 and blue/green/purple accessibility/overflow cases passed. The first
harness attempt failed because it read an API response body after navigation;
response bodies are now captured immediately, and the subsequent complete
journey passed. The failed evidence is retained, not counted as a product pass.

After that acceptance browser closed, the same lease and RADIUS session stayed
active through 19 further verified normal-worker renewal operations. A read-only
SQLite query selected operations created after browser closure; inspection found
zero Chrome processes in the client namespace. No subscription reset or periodic
reconnection was used. This is actual virtual-router evidence, independent of
WebSocket watchers, and does not close the target-router or hard-calendar gaps.

A separate authenticated loopback viewer now permits owner interaction with a
full browser in that namespace. It is a local development fixture, not a product
service or router deployment. The owner trial retains the same independent
ledger. Do not run disruptive automated exhaustion/outage probes on it while
it is reserved for manual testing. Exhausted and reconciliation failure-return
journeys, final source verification and delivery still remain required here.
