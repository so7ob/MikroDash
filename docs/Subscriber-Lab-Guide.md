# Subscriber Management lab guide

This branch is a development preview. Keep production on the existing authentication path.
Read [Subscriber Management](Subscriber-Management.md) for current limitations before using
any runtime option. No instructions here authorize writes to a live router.

## Build and isolated checks

```sh
go run ./cmd/webbuild -dir web
go build -o /tmp/mikrodash-subscriber ./cmd/mikrodash
go test ./internal/subscriber ./internal/server ./internal/db ./internal/guard ./internal/rbac
go test ./internal/subscriber -run '^$' -fuzz '^FuzzAccountingMonotoneReplay$' -fuzztime=5s -parallel=2
go test ./internal/subscriber -run '^$' -fuzz '^FuzzPeriodContainsTimestamp$' -fuzztime=5s -parallel=2
sh tools/verify.sh
```

Browser acceptance has an explicit build tag and fails when its harness is missing. Install
Playwright and @axe-core/playwright into an external temporary directory, then build web first:

```sh
SUBSCRIBER_BROWSER_MODULE=/path/to/external/node-project \
SUBSCRIBER_EVIDENCE_DIR=/tmp/subscriber-evidence \
go test -tags=subscriberbrowser ./internal/server -run '^TestSubscriberBrowserAcceptance$' -count=1 -v
```

`SUBSCRIBER_CHROMIUM` optionally selects an installed Chromium binary. Only the synthetic
httptest browser context accepts its self-signed test certificate. Production listeners
require a valid TLS key/certificate and TLS 1.2 or later; no insecure TLS default was added.
The test uses a temporary SQLite database, no real subscribers, and no real router.

## Read-only inspection

Use the configured router ID or stored label. Credentials stay inside the existing encrypted
store. Output can contain private network topology and account names: keep it out of public
issues and fixtures.

```sh
go run ./cmd/subscribercheck -data /path/to/existing/data -router '<stored ID or label>'
go run ./cmd/subscribercheck -data /path/to/existing/data -router '<stored ID or label>' -user-manager
```

The second command preserves original values and relations and flags selected naming
contradictions. It does not import credentials, balances or accounts. Newer menu properties
must be checked against the actual router's reply. Review both input access to a router-local
service and walled-garden forwarding. Do not put the subscriber bridge in LAN to bypass policy.

## Try administration before network setup

Start the current tested build with the existing development arguments; no
`-subscriber-config` is needed for administration. If a debugger is running an
older binary, stop and start it through the debugger after rebuilding the UI.
Do not start a second process against the same writable data directory.

Open **Subscriber Management**. The persistent **Network runtime: Disabled** status
means that portal/RADIUS admission is off, while the authorized administration
forms remain usable. Create a clearly named test subscriber, save an immutable
plan with separate time and download limits, assign that plan and use Inspect.
A first-login subscription must remain pending, with zero usage and full remaining
entitlement. Preparing these records does not connect a device or change RouterOS.
The local setup API is protected by existing admin authentication, the global
`system:subscribers` permission, Origin checks and reasoned audit.

## Runtime configuration shape

The command defaults to Disabled. `-subscriber-config /private/path/subscribers.json` enables
only explicitly configured services. Secrets use `secretSealed`, encrypted with the existing
MikroDash store key. Create a template with an empty `secretSealed`, then seal a secret read
from a protected file or secret-manager pipe. Do not place plaintext secrets in JSON, JavaScript,
shell arguments or fixtures. The tool refuses existing output files and does not contact a router.

```sh
go run ./cmd/subscriberconfig -data /path/to/existing/data \
  -in /private/template.json -out /private/subscribers.json \
  -router 'exact-stored-router-id' -network C < /private/radius-secret
```

The source must be an initialized MikroDash data directory. Output permissions are 0600 and
the existing encryption key is retained. The command does not enable the feature or modify the
router. Keep both the input secret and encrypted output outside the public repository.

```json
{
  "mode": "shadow",
  "portalListen": "127.0.0.1:9444",
  "portalOrigin": "https://portal.example.test:9444",
  "tlsCert": "/private/path/portal-chain.pem",
  "tlsKey": "/private/path/portal-key.pem",
  "authListen": "127.0.0.1:11812",
  "accountingListen": "127.0.0.1:11813",
  "clients": [{
    "routerId": "existing-router-id",
    "network": "C",
    "address": "192.0.2.1",
    "calledStationId": "synthetic-hotspot",
    "secretSealed": "SEALED_BY_EXISTING_STORE",
    "disconnectPort": 3799,
    "loginUrl": "https://hotspot.example.test/login"
  }],
  "pilotAccounts": [],
  "allowCoA": false,
  "allowDisconnect": false,
  "localLimitsVerified": false
}
```

The example uses documentation addresses and does not describe a deployable production
configuration. Shadow refuses network authentication and issues no disconnects. Pilot accepts
exactly one explicit account and one NAS/network and requires both a disconnect grant and
operator-confirmed local-limit capability. Managed is not acceptance-ready. Do not set these
flags merely to get past startup checks.

The subscriber listener has its own route set. If a reverse proxy is used, route only that
listener, configure its trusted source address explicitly, retain the real client address, and
verify that `/api/routers`, `/api/users`, `/app.js` and admin WebSocket rooms remain inaccessible.
A reverse proxy alone does not establish correct HotSpot client identity or walled garden access.

## C-first acceptance and rollback boundaries

Before requesting a live pilot, prepare an exact configuration diff and rollback using current
router reads. Confirm the test account has no active production session. Review RADIUS server
ordering and local user precedence so User Manager and MikroDash cannot both authorize/debit
that account. Reconcile its last User Manager record and approve each period opening balance;
that import/apply workflow remains unimplemented, so do not migrate a real account yet.

The live test must cover actual browser HTTPS POST, ticket consumption, actual HotSpot session,
one-direction upload and download, local time/byte limits, partial-use reconnect, simultaneous
admission, expiry, speed readback, browser closure, lost Stop, app/NAS restart and outage. Test
CoA timeout semantics explicitly before implementing lease extension. A 60-second delegation
is the current safety bound; it is not a transparent continuous internet service.

Do not modify A or B. Do not replace firewall rules, certificates, RADIUS entries, volumes or
User Manager records. No automated router setup/rollback script is supplied yet: its inputs
and exact C-only diff must first be resolved by the setup wizard work.

Stopping the preview closes its HTTP/RADIUS listeners and worker but retains all tables and
history. It does not restore RouterOS settings. Existing locally delegated sessions remain
subject to their local bounds. Revert an explicitly authorized C configuration using its saved
narrow diff, restore the single authentication authority, and reconcile outstanding accounting
before enabling new admission. Never delete the subscriber tables or `/data` to roll back.

## Continuous-delegation capability probes

Migration 39 adds durable operations, observations, source timestamps and unresolved quota
holds. Back up an isolated data copy before migration testing. Normal disabled startup does
not start the subscriber worker. The C pilot still needs separate approval.

Keep `allowCoA` false until the exact NAS has been probed. A `capabilities` object on its
network enrollment records `version` (exact resource-version reply), `evidence` (private
probe record), `timeoutMode` and `byteMode` (`total` or `remaining`), `counterMode`
(`input-upload` or `output-upload`), and independently proven `coaRate`, `coaGigawords`
and `coaTotal`. Missing evidence does not default to supported. Continuous renewal currently requires both timeout and byte modes to be `total`. Relative modes remain a code gap because an uncertain delayed retry can regrant consumed quota. CoA total/gigaword support
must be proved separately; their availability at admission does not prove live modification.

Probe extension and shortening on the same expendable C session. Record uptime and
session-time-left before/after, then the actual expiry. For byte changes record cumulative
session octets and local limit readback before and after. For direction, upload only and then
download only. For rate changes record the single dynamic host-target queue and both halves
of max-limit. An ACK without corresponding state is a failed probe. Never enable a capability
because its name appears in documentation or because a synthetic NAS passes.

The worker saves intent and reserves its maximum local exposure before sending CoA. A restart
reads state before retrying. A proven absent session releases its seat; unmeasured exposure
remains an explicit hold until final accounting or the pending administrative settlement flow.
Calendar-boundary continuous renewal and boundary-byte settlement still require completion.
