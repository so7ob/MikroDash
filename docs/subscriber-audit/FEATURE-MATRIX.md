# Subscriber acceptance matrix

Snapshot: 2026-11-13 (Asia/Aden). Initial code baseline ba39515f; delivered changes and exact test/CI evidence are in PROGRESS.md and TEST-EVIDENCE.md. This decomposes the requested A-N axes into named acceptance checkpoints; rows are not equal-sized work units and are not a completion percentage. A Partial row can have tested backend primitives while its requested complete workflow remains absent. Paths without a prefix below are under internal/subscriber/. The detailed original evidence and remaining gap for each module are in FEATURE-INVENTORY.md.

States: Implemented & Verified; Implemented but Not Hardware Verified; Partial; Missing; Broken; Blocked; Not Applicable. Network-dependent code never earns physical acceptance from synthetic or CHR results. The #77-#81 narrow increments are delivered with successful PR and post-merge CI in PROGRESS; the complete platform remains unfinished.

Owner policy confirmed 2026-11-13: renewal/plan change starts a new period without carryover, retaining historical subscriptions and usage. Carryover is therefore Not Applicable under the selected policy. Existing running sessions and unresolved accounting still need reconciliation; a financial payment or UI confirmation cannot independently establish applied network state.

## Axis A

Roadmap: P0-CREDENTIAL, P0-HISTORY, P1-DIRECTORY, P1-ENTITLEMENT. Priority: P0/P1. Code evidence: `storage.go; account_name.go; account_status_batch.go; internal/server/subscribers.go; web/src/pages/subscribers.ts`.

Acceptance/test contract: Protected admin UI/API/SQLite round trip, selected identity/revision, Unicode, rollback, no unrelated account/usage changes; credential reset also revokes old credentials and portal/ticket sessions.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| A01 | Create subscriber | Implemented & Verified | Scoped software/browser regression evidence in TEST-EVIDENCE; broader module remains open. |
| A02 | Edit subscriber metadata | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A03 | Manage username and login credentials | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A04 | Secure password reset | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| A05 | Activate suspend reactivate | Implemented & Verified | Scoped software/browser regression evidence in TEST-EVIDENCE; broader module remains open. |
| A06 | Renew commercial subscription | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A07 | Extend validity | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| A08 | Top up time or bytes | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| A09 | Upgrade or downgrade plan | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A10 | Explicit plan-change credit policy | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A11 | Creation edit renewal timeline | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A12 | Historical subscriptions view | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| A13 | Group site reseller assignment | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| A14 | Status plan site classification | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A15 | Advanced server search sort filters | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A16 | Reviewed safe bulk operations | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A17 | Account import and export | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| A18 | Manage linked devices | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A19 | Actual connection state | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A20 | Time down up remaining balances | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A21 | Authentication failure reasons/history | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| A22 | Subscriber usage history | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A23 | Administrative audit history | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| A24 | Independent subscriber detail page | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |

## Axis B

Roadmap: P0-PLAN. Priority: P0. Code evidence: `policy.go; storage.go:SavePlan; web/src/pages/subscriber-plan-limits.ts; internal/server/subscriber_plan_test.go`.

Acceptance/test contract: Draft UI to immutable SQLite plan and evaluated limits; exact decimal/period boundaries; existing subscription version retained; NAS application requires separate target proof.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| B01 | Time plans | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B02 | Data plans | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B03 | Combined time and data | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B04 | Unlimited quotas | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B05 | Daily weekly monthly periods | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B06 | Validity from assignment | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B07 | Validity from first login | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B08 | Separate download upload caps | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B09 | Total cap | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B10 | Calendar reset | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B11 | Concurrent session cap | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B12 | Independent down up rates | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B13 | Multiple speed choices | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B14 | Day/hour access schedules | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B15 | Network restrictions | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B16 | FUP throttle | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B17 | Expiry/exhaustion disconnect or refusal | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B18 | Audited policy exceptions | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| B19 | Copy plan/new immutable version | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B20 | Archive without invalidating subscriptions | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| B21 | Preview draft before save | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| B22 | Simulate subscriber policy | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B23 | Structured limits speeds schedule editor | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| B24 | Preserve historical plan references | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |

## Axis C

Roadmap: P0-RADIUS. Priority: P0. Code evidence: `radius.go; accounting.go; receipts.go; coa.go; runtime.go; operations.go; internal/server/subscriber_observer.go`.

Acceptance/test contract: Authenticated UDP/SQLite fault/retry/restart tests plus physical protocol/device/firmware capture; ACK is insufficient without readback; preserve accounting uncertainty.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| C01 | RADIUS authentication | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C02 | RADIUS accounting | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C03 | Access Accept and Reject | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C04 | Accounting Start Interim Stop | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C05 | CoA | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C06 | Disconnect Request | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C07 | Session Timeout | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C08 | Idle Timeout | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| C09 | MikroTik Rate Limit | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C10 | Durable session tracking | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C11 | NAS management | Partial | Existing config/single-account primitive is insufficient for the complete requested workflow; apply the axis contract. |
| C12 | Protected shared secrets | Partial | Existing config/single-account primitive is insufficient for the complete requested workflow; apply the axis contract. |
| C13 | Retries and duplicate handling | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C14 | Session reconciliation | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C15 | Network failure and recovery | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C16 | Server restart recovery | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C17 | Missing Stop accounting reconciliation | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C18 | Time and byte exhaustion | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C19 | No duplicate debit | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C20 | Directional counter accuracy | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C21 | Enforcement without dashboard | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| C22 | Measured authorization latency | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| C23 | Bounded router resource cost | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |

## Axis D

Roadmap: P1-PPPOE, P0-RADIUS. Priority: P1. Code evidence: `radius.go; runtime.go; readiness.go; internal/server/subscriber_observer.go; internal/server/subscriber_portal.go`.

Acceptance/test contract: Separate HotSpot and PPP fixtures/auth/counters/addressing/CoA/disconnect; no PPP claim from HotSpot or CHR alone.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| D01 | Complete HotSpot administration | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| D02 | Explicit PPP NAS/service setup | Missing | No independent PPP service/obinternal/server/addressing and acceptance workflow exists in the cited HotSpot implementation. |
| D03 | PPP authentication | Missing | No independent PPP service/obinternal/server/addressing and acceptance workflow exists in the cited HotSpot implementation. |
| D04 | PPP RADIUS profiles | Missing | No independent PPP service/obinternal/server/addressing and acceptance workflow exists in the cited HotSpot implementation. |
| D05 | PPP static/dynamic IP | Missing | No independent PPP service/obinternal/server/addressing and acceptance workflow exists in the cited HotSpot implementation. |
| D06 | PPP accounting | Missing | No independent PPP service/obinternal/server/addressing and acceptance workflow exists in the cited HotSpot implementation. |
| D07 | PPP speeds and limits | Missing | No independent PPP service/obinternal/server/addressing and acceptance workflow exists in the cited HotSpot implementation. |
| D08 | PPP disconnect | Missing | No independent PPP service/obinternal/server/addressing and acceptance workflow exists in the cited HotSpot implementation. |
| D09 | PPP concurrent sessions | Missing | No independent PPP service/obinternal/server/addressing and acceptance workflow exists in the cited HotSpot implementation. |
| D10 | Independent HotSpot/PPPoE acceptance | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |

## Axis E

Roadmap: P1-VOUCHER. Priority: P1. Code evidence: `storage.go:CreateAccount; internal/server/subscribers.go:create; no batch/inventory/delivery tables in current Schema`.

Acceptance/test contract: Atomic accounts/subscriptions/audits, unique idempotent issuance, encrypted response-loss recovery until delivery ACK; authorized export/print/inventory; no secrets in ordinary reads/logs.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| E01 | Single voucher | Implemented & Verified | Scoped software/browser regression evidence in TEST-EVIDENCE; broader module remains open. |
| E02 | Chunked hundreds/thousands issuance | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E03 | Choose count | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E04 | Choose immutable plan | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| E05 | Voucher validity | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| E06 | Code length and generation method | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| E07 | Unique codes | Partial | Existing config/single-account primitive is insufficient for the complete requested workflow; apply the axis contract. |
| E08 | Referenced batches | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E09 | Print template | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E10 | A4 printing | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E11 | Multiple cards/page | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E12 | QR codes | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E13 | Custom local logos/design | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E14 | CSV and PDF | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E15 | Issued used unused state | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E16 | First-use date | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| E17 | Batch reporting | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E18 | Assign reseller | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E19 | Cancel unused vouchers | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E20 | Secret delivery authorization | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| E21 | Inventory | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E22 | Low-stock alerts | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| E23 | Sale receipt/card | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |

## Axis F

Roadmap: P1-SESSIONS. Priority: P1. Code evidence: `storage.go:Snapshot; observations.go; connections.go; internal/server/subscriber_socket.go; coa.go`.

Acceptance/test contract: Scoped bounded session query/push and control at use, exact device ownership, timestamp/quality and applied-state evidence; no synthetic rate labelled live.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| F01 | Global active sessions | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F02 | Subscriber identity | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F03 | Router identity | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F04 | HotSpot/PPP service identity | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F05 | IP address | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F06 | Available MAC | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F07 | Start time | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F08 | Connected duration | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F09 | Consumed bytes | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F10 | Measured transfer rates | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F11 | Applied speed | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F12 | Accounting status | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F13 | Measurement freshness/quality | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F14 | Owned disconnect | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F15 | Verified live speed change | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| F16 | Push updates without reload | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |

## Axis G

Roadmap: P1-ENTITLEMENT. Priority: P1. Code evidence: `storage.go:Assign; delegation.go; subscriber_subscriptions and subscriber_audit; no commercial operation request-key/receipt schema`.

Acceptance/test contract: Confirmed explicit fresh-period/no-rollover review; exact request-key receipt/replay, concurrent renewal and rollback; historical subscriptions/usage never rewritten.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| G01 | Issue commercial subscription | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| G02 | Start/end timestamps | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| G03 | First-login activation | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| G04 | Renew before/after expiry | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| G05 | Extend | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| G06 | Top up | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| G07 | Carry remaining credit | Not Applicable | Owner-selected fresh-period/no-rollover policy; retain full history. |
| G08 | Exhaustion stop | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| G09 | Restore after confirmed renewal | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| G10 | Renewal/edit history | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| G11 | Idempotent retry | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| G12 | Bulk renewals | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| G13 | Operation receipt | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| G14 | Separate commercial usage and applied state | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |

## Axis H

Roadmap: P2-BILLING. Priority: P2. Code evidence: `storage.go:Schema; internal/server/subscribers.go action list; no subscriber financial schema/API`.

Acceptance/test contract: Exact currency minor units, immutable documents/reversals, idempotent references and server authority; keep financial and network usage ledgers logically distinct in same SQLite.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| H01 | Prices/currencies | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| H02 | Priced products/plans | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| H03 | Invoices | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| H04 | Sale receipts | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| H05 | Cash payments | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| H06 | Settlements | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| H07 | Customer balance | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| H08 | Controlled cancellations/refunds | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| H09 | Partial payments | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| H10 | Daily/monthly finance reports | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| H11 | Future gateway adapters | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| H12 | Financial document deduplication | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| H13 | Complete financial audit | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |

## Axis I

Roadmap: P2-RESELLER. Priority: P2. Code evidence: `subscriber_accounts has no reseller ownership; rbac system:subscribers is global; no reseller ledger/stock API`.

Acceptance/test contract: Persisted ownership predicates on every read/write/export; no cross-reseller stock/secret/data access; atomic balance/stock limits and audit.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| I01 | Reseller account | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| I02 | Reseller grants | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| I03 | Voucher allocation | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| I04 | Owned stock | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| I05 | Sales | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| I06 | Balance | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| I07 | Commissions | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| I08 | Financial/operating limits | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| I09 | Reports | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| I10 | Server-enforced data scope | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| I11 | Audit | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |

## Axis J

Roadmap: P2-EVENTS. Priority: P2. Code evidence: `policy.go alert is decision reason; no subscriber transactional event outbox; reuse internal/mailer, notify, alertdispatch`.

Acceptance/test contract: Transactional event outbox, unique recipient/event keys, delivery log and bounded crash-safe retry; unavailable channels remain disabled.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| J01 | Expiry warning | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| J02 | Data exhausted | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| J03 | Time exhausted | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| J04 | Renewal success | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| J05 | Repeated login failures | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| J06 | RADIUS outage | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| J07 | NAS outage | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| J08 | Late accounting | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| J09 | Reconciliation needed | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| J10 | Configured delivery channels/log/retry/dedup | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |

## Axis K

Roadmap: P2-PORTAL. Priority: P2. Code evidence: `internal/server/subscriber_portal.go; web/src/subscriber-view.ts; subscriber-login.ts; subscriber-qr.ts; local subscriber-portal.css/Tajawal`.

Acceptance/test contract: Actual protected portal HTTP/SQLite browser journey, owned data/CSRF/revocation, Arabic RTL/Latin digits/local fonts, mobile/keyboard/axe; network state separately verified.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| K01 | Current subscription | Implemented & Verified | Scoped software/browser regression evidence in TEST-EVIDENCE; broader module remains open. |
| K02 | Remaining balance | Implemented & Verified | Scoped software/browser regression evidence in TEST-EVIDENCE; broader module remains open. |
| K03 | Expiry | Implemented & Verified | Scoped software/browser regression evidence in TEST-EVIDENCE; broader module remains open. |
| K04 | Usage | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| K05 | Owned devices/sessions | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| K06 | Entitled speed choice | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
| K07 | Secure credential change | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| K08 | Renewal request | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| K09 | Redeem top-up voucher | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| K10 | Receipts | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| K11 | Subscription history | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| K12 | So7ob Arabic RTL/local assets/mobile | Implemented & Verified | Scoped software/browser regression evidence in TEST-EVIDENCE; broader module remains open. |
| K13 | Isolation from administrator | Implemented & Verified | Scoped software/browser regression evidence in TEST-EVIDENCE; broader module remains open. |

## Axis L

Roadmap: P1-IMPORT. Priority: P1. Code evidence: `readiness.go:PreviewUserManager; #79 import_preview.go/tests; no migration apply/rollback API`.

Acceptance/test contract: Reviewed source fingerprint/mappings/current-period conversion, chunk transaction and stable idempotency; resume/rollback preserves newly accounted usage and reports errors.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| L01 | Read-only preview | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| L02 | Duplicate detection | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| L03 | Account mapping | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| L04 | Plan mapping | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| L05 | Explicit current-period balances | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| L06 | Suspended accounts | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| L07 | Import error log | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| L08 | Small apply chunks | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| L09 | Resume | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| L10 | Idempotency | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| L11 | Controlled rollback | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| L12 | Compatibility test before approval | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |

## Axis M

Roadmap: P2-REPORTS, P3-LOAD. Priority: P2/P3. Code evidence: `storage.go:Accounts/Snapshot; generic internal/reports and reportpdf; no aggregated subscriber reporting API`.

Acceptance/test contract: Bounded scoped aggregate SQL/export with date/site/plan filters, explicit confirmed/held usage, query plans and measured synthetic populations; no full browser directory scan.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| M01 | Subscriber count | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| M02 | Active accounts | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| M03 | Suspended accounts | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| M04 | Expired subscriptions | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| M05 | Currently online | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| M06 | Counts by plan | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| M07 | Daily/monthly usage | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| M08 | Usage per subscriber | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| M09 | Usage per NAS | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| M10 | Popular plans | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| M11 | Sold/used vouchers | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| M12 | Finance enabled revenue/collections | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| M13 | Reseller performance | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| M14 | Failed authentication | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| M15 | Accounting/RADIUS quality | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| M16 | Pending reconciliation | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| M17 | Export | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| M18 | Date/site/plan filters | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| M19 | Scalable SQL aggregation | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |

## Axis N

Roadmap: P2-SETUP. Priority: P2. Code evidence: `runtime.go:Config.Validate; readiness.go:Inspect; cmd/subscribercheck; internal/guard; generic store/sites and rbac`.

Acceptance/test contract: Protected staged setup, secrets never read back, version-qualified capability proof, command review/rollback and isolated restore tests before activation.

| Feature ID | Requested checkpoint | Current state | Evidence / remaining acceptance |
|---|---|---|---|
| N01 | Add NAS | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N02 | Connectivity test | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N03 | Write-only RADIUS secret setup | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N04 | Choose networks | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N05 | Auth test | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N06 | Accounting test | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N07 | Disconnect/CoA tests | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N08 | RouterOS version compatibility | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N09 | Per-device capability evidence | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N10 | Actual runtime status | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N11 | Sites/branches | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| N12 | Permission setup | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N13 | Subscriber backup/restore | Missing | No complete route/schema/UI workflow found in cited module; implement and test the contract above. |
| N14 | Activation readiness | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N15 | Command preview | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N16 | Explicit rollback | Partial | Existing primitive is partial; complete the requested workflow and relevant test contract above. |
| N17 | Existing guard enforcement | Implemented but Not Hardware Verified | Pending Hardware Verification on target; see #76 and existing protocol tests. |
