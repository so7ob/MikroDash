# Security review

Snapshot: 2026-11-13 (Asia/Aden). Baseline `ba39515f003484211afe3dfef783ade6dc39f813` on `develop`; `main` = `ca2b1d5284d2da98967a5bc482282ee85d02bc8e`. Issue #95, parent #38. Source inspection is not an executed test or physical-device acceptance.

Existing controls: `subscribers.go: subscriberAdmin` requires a modern administrator session and `system:subscribers`; mutations additionally require same-Origin and `X-MikroDash-Request`. Portal listener/session registry/routes are separate. `radius.go` bounds authentication concurrency, restricts NAS peers and validates authenticators. `runtime.go` and `coa.go` gate authority and exact owned targets. Subscriber passwords use existing scrypt; voucher hashes use high-entropy random secrets. NAS plaintext secrets have `json:"-"`; sealed config uses existing store encryption. Browser-bound errors pass through `safe.Message`.

Findings requiring work are complete request validation and decimal parsing (#77), credential rotation/session revocation, durable recovery of batch secrets with authorization and acknowledgement, commercial idempotency, and row-level reseller scope. The current global admin grant cannot safely become a reseller grant. Audit histories must exclude passwords/voucher secrets and retain exact IDs; no secret-bearing payload belongs in logs, URLs, fixtures or normal GETs.

Sensitive findings must follow SECURITY.md rather than public exploit disclosure. This review records functional control gaps and existing public PR remediation. Negative HTTP/session/Origin/NAS/device tests are necessary at the real call site; hiding controls in the frontend provides no authorization.

Keep runtime disabled/Shadow until explicit target admission. Never turn on allowCoA/allowDisconnect or capability flags to make a test pass. No live gateway, NAS write, host-route change, production deployment or authentication cutover is part of this audit. Preserve existing /data cryptography, bare-array users.json and operator accounts.

Delivered controls: #77 now validates the full bounded command and exact decimal
quantities; #80/#81 expose protected no-store status history with exact IDs and
escaped original evidence. General audit and credential rotation remain missing.
No secret is included in new status or benchmark payloads. Native disclosure
contains the same escaped source record, and parsed fields require a complete
recognized envelope before explaining a transition.
