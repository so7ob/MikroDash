# Proven gaps and testable work

Snapshot: 2026-11-13 (Asia/Aden). Baseline `ba39515f003484211afe3dfef783ade6dc39f813` on `develop`; `main` = `ca2b1d5284d2da98967a5bc482282ee85d02bc8e`. Issue #95, parent #38. Source inspection is not an executed test or physical-device acceptance.

| Gap | Evidence | Development task and acceptance |
|---|---|---|
| Decimal quantities | `policy.go: Quantity.UnmarshalJSON` uses `fmt.Sscan`; PR #77 replaces this with explicit decimal parsing | Integrate #77 after current-base tests. `010` becomes 10, `08` becomes 8, overflow leaves destination unchanged. |
| Incomplete JSON command validation | `subscribers.go: subscriberBody` decodes only one value | #77 must reject trailing objects/garbage/null/interruption/oversize before state changes; authority still runs first. |
| Plan draft editor | `web/src/pages/subscribers.ts` appends limits without edit/remove | Review #78, preserve exact decimal quantities and immutable versions; browser add/edit/cancel/remove and interruption checks. |
| Import source quality | `readiness.go: PreviewUserManager` concatenates unversioned material | Review #79 diagnostics/projection/versioned fingerprints. Do not mistake preview for migration apply. |
| Status history | base has audit rows but no history read endpoint/UI | Deliver #80 then retarget #81 to develop after dependency integration; test exact cursors, owner/account isolation, stale reads and keyboard. |
| Credentials | only CreateAccount/Authenticate; no rotate/reset path | Scoped audited reset, old-secret revocation and portal-session invalidation; no secret readback; concurrent/admin/portal guard tests. |
| Scale | Accounts reads all rows, frontend sorts/filters all | Bounded SQL pagination/search/sort with indexes and scoped counts. 100/1k/10k/100k synthetic measurement; UI never fetches whole directory. |
| Commercial operations | Assign creates subscription without request identity | Idempotent audited commercial operation/receipt, explicit reset/rollover policy and conflict-safe retry; concurrent renewal tests. |
| Voucher delivery | one-time CreateAccount response only; no batch table | Atomic bounded batch plus encrypted recoverable delivery until explicit receipt; lost response replay cannot duplicate or lose codes. Then chunked parent jobs, inventory/print/export. |
| PPPoE | NAS and observations depend on HotSpot; CHAP rejected | Independent PPP service and auth policy; no claim of PPP support from common RADIUS tests. |
| Physical verification | #76 open, no current L009/C evidence | Keep Pending Hardware Verification. Review target/version/scope, commands and rollback before separate live approval. |

All other A-N gaps map to IDs in DEVELOPMENT-ROADMAP.md; no empty navigation items are accepted as delivery. #94 was closed `not_planned`, not implemented; its one-time-response scope cannot meet lost-delivery acceptance.

## Current disposition

#77 fixes decimal and complete-command input at 149123b7. #78 delivers the
structured draft editor at b29ee7d6, with actual HTTP/SQLite version/FUP tests.
#80/#81 deliver status API/UI at ca6d5e95/4f1828c7; broader audit remains open.
#79 source review is delivered at 4a000c0c with full current-base and post-merge CI; migration apply/rollback remains
missing. The owner approved fresh commercial periods without rollover, retaining
history. Idempotent operations/receipts and credential epochs/revocation remain
P0 work. Nothing here closes target acceptance #76 or the full platform #38.
