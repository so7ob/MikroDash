# Optional HotSpot login domain

Issue #72 is in progress. The initial transport passed actual owned-CHR browser, speed, five-renewal and
accounting acceptance. Its timeout test exposed cross-authority fallback. The
strengthened background routing guard is implemented and covered by automated
tests; its final normal-runtime CHR replay remains pending. No target C change
or acceptance is implied.

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

Target UDP delivery, actual local User Manager transition compatibility and the
final guarded-runtime session/restart acceptance remain explicit steps. The tool issues read-only RouterOS commands and changes no ledger.

The current operator C runtime remains Shadow. Existing User Manager, A/B and
HotSpot pages are unchanged. A concrete reviewed pilot and its rollback need
separate operator authorization before live writes.
