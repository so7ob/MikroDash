# Optional HotSpot login domain

Issue #72 is in progress. The configuration, portal handoff and authenticated
RADIUS domain checks are implemented and covered by focused automated tests.
Actual domain session accounting, renewal, speed changes and finality still need
the owned-CHR acceptance run. No target C change or acceptance is implied.

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
It never grants admission or sets capability flags. The review requires current
identity/time, exact HTTPS/accounting/split/default-domain profile metadata,
ordered domain endpoint and response-authenticator settings, both explicit ports,
local-account precedence and a complete legacy username inventory. Missing,
unavailable or incomplete fields block a match. Legacy names with separators or
a pilot-name collision require review without renaming or changing those names.

A NAS transport timeout can behave differently from Access-Reject. Prove that
fallback cannot authorize the pilot through another authority during outage;
ordering alone is insufficient. Target UDP delivery, actual session identity,
local quota/timeout behavior, renewal, speed readback and rollback remain explicit
acceptance steps. The tool issues read-only RouterOS commands and changes no ledger.

The current operator C runtime remains Shadow. Existing User Manager, A/B and
HotSpot pages are unchanged. A concrete reviewed pilot and its rollback need
separate operator authorization before live writes.
