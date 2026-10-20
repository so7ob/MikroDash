# Subscriber portal behind an HTTPS reverse proxy

This transport is implemented for the isolated subscriber listener. It does not
turn on network admission or configure a router. Full acceptance remains open in
[#38](https://github.com/so7ob/MikroDash/issues/38).

## Two explicit transports

The default is native TLS (`portalTransport` omitted or `"tls"`). It still requires
`tlsCert` and `tlsKey` and serves TLS 1.2 or later. No missing certificate triggers
an HTTP fallback.

RouterOS's [reverse proxy documentation](https://manual.mikrotik.com/docs/network-management/proxy/reverse-proxy/)
describes HTTPS termination and a plain HTTP/1.1 backend, with the original Host,
X-Forwarded-For and X-Forwarded-Proto. The current manual says 7.26. Read-only
inspection found an existing proxy rule on the target's 7.24.5, but its headers
and this subscriber path have not been accepted on that target. Verify them in
Shadow before permitting admission.

Set `portalTransport` to `"trusted-proxy-http"` only for a trusted private backend.
This mode requires:

- An exact HTTPS public `portalOrigin`, including its nondefault port.
- A literal private or loopback `portalListen` address. Wildcards, hostnames and
  public bind addresses are rejected. Port zero is available for isolated tests;
  use a fixed port in a deployment.
- One to eight exact private or loopback `portalProxyPeers`. CIDRs, duplicates,
  unspecified addresses and public peers are rejected.
- Empty/omitted `tlsCert` and `tlsKey`: the approved edge owns the TLS identity.
- An isolated app/router bridge or loopback network. Peer-IP checks do not encrypt
  the backend traffic or replace network isolation. Use native TLS across an
  untrusted backhaul.

The proxy backend is a separate HTTP server with only subscriber routes. Pointing
a proxy at the administrative listener is not supported. The app does not change
global trusted-proxy settings for this transport.

## Private configuration template

These are **synthetic example values**, not the operator's topology. Replace them
with current approved values before sealing. Keep the file outside the public
repository. This example starts in Shadow and issues no CoA or Disconnect.

```json
{
  "mode": "shadow",
  "portalTransport": "trusted-proxy-http",
  "portalListen": "10.203.0.2:38101",
  "portalOrigin": "https://portal.example.test:9443",
  "portalProxyPeers": ["10.203.0.1"],
  "authListen": "10.203.0.2:11812",
  "accountingListen": "10.203.0.2:11813",
  "clients": [{
    "routerId": "existing-router-id",
    "network": "C",
    "address": "10.203.0.1",
    "calledStationId": "synthetic-hotspot",
    "secretSealed": "",
    "disconnectPort": 3799,
    "loginUrl": "https://hotspot.example.test/login"
  }],
  "pilotAccounts": [],
  "allowCoA": false,
  "allowDisconnect": false,
  "localLimitsVerified": false
}
```

Use the existing [secret-sealing command](Subscriber-Lab-Guide.md#runtime-configuration-shape)
and existing `/data` key; never put a plaintext RADIUS secret in JSON or shell
arguments. Add `-subscriber-config /private/subscribers.json` to an explicitly
approved deployment. This does not switch the router's RADIUS authority.

## Request and identity boundary

All routes, assets and WebSocket upgrades require an exact trusted TCP peer,
the configured public Host, one `X-Forwarded-Proto: https` field, and a valid
`X-Forwarded-For` chain. Missing, duplicate, malformed, unspecified or multicast
client addresses are rejected. The header is limited to 4096 bytes and 32 hops.
The shared `internal/trustedproxy` parser walks from the right. A browser's
leftmost injected address cannot replace the actual client appended by the edge.
A chain resolving only to the trusted proxy itself is refused.

The resulting address is request-scoped. Both the sign-in limiter and HotSpot
device lookup use it; no client MAC/IP field grants a ticket. The backend still
reads the enrolled router's host table to select the device and network. A portal
session remains separate from HotSpot admission.

Host-only `__Host-mikrodash_subscriber` cookies remain Secure/HttpOnly. CSRF,
Origin, account ownership and WebSocket checks remain in effect at the HTTPS
origin. The backend does not serve `/api/routers`, `/api/users`, `/api/subscribers`,
`/app.js` or administrator socket rooms.

## C pilot review and rollback

Prepare a private review sheet before any live write. Record current router ID,
version/architecture, exact C server/profile/interface, app address/port, proxy
source address and public origin/certificate. Include the current rule ID and
its old backend/certificate/disabled state, current input/walled-garden decisions,
and active C sessions. Never export a private certificate key to make this mode
work; the TLS key can stay at the router.

For an approved existing proxy rule, the candidate API diff changes only its
backend `ip-address` and `port`. Render the exact old/new values in the review.
Keep SNI, certificate and service port unchanged. Refuse a stale or ambiguous
rule match. If the rule serves User Manager or other production traffic, record
that impact and obtain a scoped window or a separately approved origin first.
Do not overwrite it automatically. A new rule likewise requires an approved
unique SNI and a certificate already valid for that origin.

Verify HTTPS reachability from a real C client both before authentication and
after exhaustion. Input access to the router's proxy and forwarding to a private
backend are separate checks; Walled Garden alone is not proof. Refuse the private
HTTP backend from an ordinary subscriber peer. Confirm the forwarded address
against the router host table and verify admin routes stay inaccessible. Missing
headers on the actual firmware are a failed probe, not a reason to relax trust.

Rollback restores exactly the recorded proxy backend/port if their values still
match the approved candidate, verifies the previous origin works, and stops only
the preview runtime after accounting is settled. Preserve `/data`, the encryption
key, ledger and new usage. Do not restore an old database over new accounting.
This transport does not alter RADIUS ordering, local HotSpot accounts or sessions;
authentication cutover needs its own approved diff and rollback. A/B are outside
the C pilot scope. No live configuration/rollback was executed by these examples.

## Automated evidence

- Real HTTP/TLS and SQLite tests bind tickets to a scripted router's actual client
  row, reject injected client/device claims and exercise the owned RADIUS handler
  over real UDP, including exact retries and one-use replay refusal.
- Native TLS and proxy HTTP both pass actual runtime startup, shared NAS retention,
  observer progress without watchers, worker cancellation and closed-listener tests.
- The browser journey runs against native TLS and HTTPS-edge/HTTP-backend paths:
  login, speed choice/pending state, Shadow refusal, logout, three themes,
  English/LTR, 375/1280 and axe. Its accounts/NAS are synthetic.
- A fuzz property requires every accepted injected-left-claim request to resolve
  to the actual appended client. A short fuzz run is not exhaustive validation.

See the [verification record](Subscriber-Verification.md) for commands/results.
Neither this lab nor a static ARMv7 build proves target execution or a working
Internet session. The original C local-limit, CoA and accounting acceptance stays
pending until the actual controlled pilot is performed.
