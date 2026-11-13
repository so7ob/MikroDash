# Credential rotation contract (#99)

This is P0-CREDENTIAL under #38, following the #97/#98 shared administrative UI.
Account identity, username, Unicode name, status, entitlement, measured usage,
network leases and reservations remain stable. The administrative action replaces
the password and revokes any voucher; subsequent login requires username and the
new password. New voucher issuance and username editing remain separate work.

A durable per-account integer credential revision starts at zero for existing
accounts. Authentication returns the exact revision that verified the credential.
The reset transaction checks the reviewed revision, advances it, replaces the
scrypt hash/salt, clears voucher authentication, expires tickets, rejects pending
connection intents, and records a secret-free account audit atomically. Revision
values are decimal strings in JSON. Overflow and stale reviews fail closed.

Portal sessions carry their authenticated revision. HTTP authorization and every
WebSocket send consult the current durable revision; memory removal is only an
optimization and removes older epochs, preserving any newly authenticated epoch.
A login that completes credential verification before a reset cannot create a
usable session afterward. A login linearized before reset can receive a response
later, but its stamped old epoch is already revoked and grants no future access.

Ticket issuance checks the portal revision inside its transaction. Tickets carry
the same epoch and redemption checks it; pre-migration tickets lacking proof fail
closed. Expire rather than delete tickets so flow/accounting history is retained.
RADIUS reserves through the existing immediate transaction with the authenticated
epoch checked before side effects or replay. An admission committed before reset
is an existing bounded network authorization. Reset does not disconnect it,
release its reservations or claim that a late NAS packet was revoked. Admission
whose transaction starts after reset cannot use the stale epoch.

The administrator uses the existing global authority and Origin/header boundary,
an explicit account, new password, reason, reviewed revision and confirmation.
No credential/hash/salt is returned or audited. The shared modal retains failed
input, prevents concurrent saves and fences unknown/conflicting responses until
refresh. Closing/discarding and successful save erase the secret from the form.

Validation refused before writing returns 400 and permits correction. A stale
review returns 409. Storage/commit failures return a safe 503 and require a fresh
review, since a client cannot infer commit outcome from a failed acknowledgement.
The schema readiness check requires the new proof tables. Migration preserves
existing hashes/vouchers/accounts; outstanding pre-epoch tickets fail closed.

## Executed verification

- Store/migration/server focused tests passed: old/new/voucher authentication,
  exact revisions above 2^53 and overflow, competing reviews, in-flight admission
  and late login response, durable HTTP/WS revocation without registry eviction,
  fresh-login preservation, real UDP RADIUS old/ticket replay refusal, audit and
  SQL rollback, authority/Origin/header/confirmation and safe storage failure.
- TypeScript and 169 frontend tests passed, including detached frozen identity,
  missing/numeric/overflow revision refusal and exact BigInt review.
- Protected HTTP/SQLite browser passed at 375/768/1280/1920 in dark/light/Nord:
  validation/cancel/dirty Escape, focus trap/return, Unicode and escaped script
  data, real concurrent 409, correctable backend 400, committed lost response,
  duplicate-submit and busy close prevention, old portal revocation/new login,
  unrelated account preservation. Twelve axe cases had zero findings; shared
  form geometry passed and rejected a deliberately broken input height. Go
  independently verified exactly three durable resets and secret-free audit.
- The existing administration/browser regression and final exact-head full gate
  are recorded on Issue #99 with PR and develop delivery evidence. Ordinary
  tests do not silently include optional browser checks.

Artifacts: /tmp/mikrodash-99-*-tests.log, /tmp/mikrodash-99-focused-final.log,
/tmp/mikrodash-99-browser-final.log, /tmp/mikrodash-99-browser-final/ and
/tmp/mikrodash-99-admin-regression.log. Representative committed images and
source/image hashes are linked from UI-UX-UNIFICATION.md.

Retained corrections: initial account scans omitted the new metadata field;
focused tests caught and corrected them. Browser review caught an unbroken long
Unicode name clipping at 375px; existing text-break now wraps it. Storage errors
were separated from validation so uncertain outcomes require a new review. No
check was deleted or weakened. Exact final gate/CI evidence is on #99 to avoid a
self-referencing documentation commit.

All protocol/browser identities and NAS observations are synthetic. Protected
HTTP, SQLite and loopback UDP RADIUS are real executed local code. No runtime
activation, live router write, release or deployment is part of this increment.
