# Optional HotSpot login domain

The optional transport and background routing guard pass automated tests and
actual normal-runtime CHR 7.24.5 browser, speed, five-renewal and final accounting
acceptance. An earlier timeout test exposed cross-authority fallback; the runtime
now refuses that configuration. No target C change or acceptance is implied.

## Identity and authority

`clients[].loginDomain` is optional server configuration. Empty retains the
existing direct username path. A configured selector is 1–63 ASCII letters,
digits, dots or hyphens, with an alphanumeric first and last character. It is a
RADIUS routing selector, not a URL or a browser preference.

For account `alice` and selector `md-pilot`, the server's connect response gives
the browser `md-pilot\alice` for the allowed HTTPS HotSpot form. The ticket,
account ID, original username, subscription, usage and expiry keep their existing
identity. No credential is copied into public configuration or a landing asset.
The subscriber still enters the original account once. The browser cannot choose
another domain through the connect request.

The NAS must normalize the username itself. Domain-mode Access-Request requires
exactly one original username, one matching MS-CHAP-Domain and one matching
Mikrotik-Realm, in addition to the existing source, Called-Station-Id and
Message-Authenticator checks. Missing, duplicate, different or unsplit identity
is rejected before the ticket is consumed. An exact accepted packet retry keeps
its lease; another packet cannot reuse the ticket.

Unicode account names are preserved. Names containing `@` or a backslash are
explicitly refused for this optional transport until their NAS interpretation is
qualified. They remain unchanged in storage and remain compatible with an empty
login domain. Portal account access remains independent of HotSpot connection.

## Read-only configuration review

The [owned CHR routing matrix](Subscriber-CHR-Lab.md#same-profile-domain-selection-measurement)
proved that list order and splitting both matter on 7.24.5. An appended endpoint
was never selected. First-position plus splitting normalized both qualified
forms while leaving plain names with the primary. That rejecting measurement
alone does not prove admission, accounting, CoA or timeout fallback.

The existing readiness tool can compare actual non-secret metadata with an
explicit router-visible endpoint. NAT endpoint ports are never inferred from the
application's bind address. Example using documentation addresses:

```sh
go run ./cmd/subscribercheck -data /path/to/data -router '<stored ID or label>' \
  -login-domain md-pilot -hotspot hs-C -subscriber pilot-account \
  -radius-endpoint 198.51.100.10:11812 -radius-accounting-port 11813
```

`routing.configurationMatches` means only that the observed metadata matches.
The runtime requires this verdict, fresh version-bound background evidence and
account checks before domain admission or new CoA writes. The CLI does not set
capability flags. The review requires current
identity/time, exact HTTPS/accounting/split/default-domain profile metadata,
ordered domain endpoint and response-authenticator settings, both explicit ports,
local-account precedence and, for explicit transition mode, a complete active
legacy username inventory. Missing,
unavailable or incomplete fields block a match. Active legacy names with separators or a pilot-name collision require review
without renaming or changing those names. Disabled historical identities are
retained and do not become a competing authority.

Actual CHR timeout tests sent a request to the next blank-domain endpoint. A
second measured trial used distinct `md-legacy` and `md-pilot` domains, with the
profile default set to `md-legacy`. Plain names reached only the legacy stand-in;
qualified names reached only MikroDash. Both qualified syntaxes timed out without
any legacy request when the pilot port was unreachable. Exact original metadata
was restored. This is NAS selection proof, not actual User Manager acceptance.

The guard rejects blank-domain fallback entries anywhere in the enabled HotSpot
list, including after the intended endpoint. Standalone domain mode requires one
enabled authority. Explicit transition mode permits that authority plus the
reviewed local User Manager endpoint, with a different domain/default domain.
For example, the private server NAS configuration can include:

```json
{
  "loginDomain": "md-pilot",
  "loginRouting": {
    "address": "198.51.100.10",
    "authPort": 11812,
    "accountingPort": 11813,
    "legacyDomain": "md-legacy"
  }
}
```

Omit `legacyDomain` for standalone operation. This requires no User Manager
package or API. A transition checks active legacy names without reading their
credentials. Existing account names, disabled histories and balances remain
unchanged. The read-only CLI accepts `-legacy-domain md-legacy` for this review.

One background reader per NAS refreshes immutable routing metadata every ten
seconds, separately from enforcement and display collectors. Cold, failed,
older-than-fifteen-second or different-version evidence blocks new domain
admission and renewal. Failed reads retain the last view for diagnosis, but do
not reuse it as permission. Existing accounting continues; already issued local
time/byte bounds remain. These are configuration timings, not measured latency
or a guarantee about external administrator changes. Account observations and
RADIUS accounting retain their separate cadences.

Administrative `GET /api/subscribers` includes cached `routing[]` metadata
freshness (`fresh`, `stale`, `unavailable`, `not-required`), observation time,
network and domain. This is not an account authorization result and triggers no
router read. The subscriber listener still exposes none of these admin routes.

The CLI issues read-only RouterOS commands and changes no ledger. Target UDP
delivery, actual local User Manager transition compatibility and guarded-runtime
outage acceptance on the target remain explicit steps.

## Measured owned-CHR acceptance

Implementation `f83b1337` ran against an isolated owned CHR 7.24.5 using the normal
server, HTTPS portal and namespace client. A fresh Unicode account entered its
credentials once, submitted the configured domain identity to HotSpot and returned
to its confirmed dashboard in 2158 ms. Both entitled speeds were applied through
CoA and verified by exact-device queue readback. The subscription, expiry and
byte balances survived both choices. Six 375/1280 viewport and blue/green/purple
theme checks passed with English/LTR markup, no overflow, no browser errors and
no axe WCAG 2 A/AA or 2.1 AA violations. Admin routes returned 404 on the portal.

With all browsers closed, five verified renewals retained the same lease and
RADIUS session. Its live sample showed 204 connection seconds. A subsequent
one-way download exhausted the simultaneous 65536-byte download allowance;
trusted final accounting committed 242 seconds, 88321 download bytes and 2636
upload bytes, with zero pending finality. The remaining time stayed 3358 seconds.
Real HTTPS re-entry was rejected while the portal and account remained available.

The accounted download exceeded the local test limit by 22785 bytes. The client
received 87840 payload bytes from a 1 MiB offered transfer and timed out after
12.019 seconds. That interval is a client deadline, not a measured disconnect
latency. Concurrent two-worker fuzz and focused race tests were running. These
samples establish behavior under that declared lab load, not an accepted target
overshoot budget, guaranteed latency or normal-load resource measurement.

An earlier account was separately tested across an application outage: local
delegation expired, and missing Stop accounting remained unresolved after restart.
Admission stayed rejected despite positive known time credit; portal access
remained available. That run predates the final routing guard and is not a replay
of its outage behavior. Its instrumentation failures and lost timing samples are
retained in the verification report. Every earlier balance, exhausted account and
unresolved finality hold was preserved. Nested and outer routing rollback restored
the exact original non-secret NAS rows/profile; the prior owned calendar runtime
was restored without signalling the operator's VS Code process.

The current operator C runtime remains Shadow. Existing User Manager, A/B and
HotSpot pages are unchanged. A concrete reviewed pilot and its rollback need
separate operator authorization before live writes.
