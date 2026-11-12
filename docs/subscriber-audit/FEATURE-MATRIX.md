# Acceptance matrix

Snapshot: 2026-11-13 (Asia/Aden). Baseline `ba39515f003484211afe3dfef783ade6dc39f813` on `develop`; `main` = `ca2b1d5284d2da98967a5bc482282ee85d02bc8e`. Issue #95, parent #38. Source inspection is not an executed test or physical-device acceptance.

This matrix links every requested A-N axis to independent roadmap groups. A grouped row is not a percent; completion requires its acceptance tests and the missing child requirements in FEATURE-INVENTORY.

| Axis | Roadmap feature IDs | Baseline state | Accepted this session |
|---|---|---|---|
| A | P0-CREDENTIAL, P0-HISTORY, P1-DIRECTORY, P1-ENTITLEMENT | Partial | None yet |
| B | P0-PLAN | Partial | None yet |
| C | P0-INPUT, P0-RADIUS | Partial | None yet |
| D | P1-PPPOE, P0-RADIUS | Partial | None yet |
| E | P1-VOUCHER | Partial | None yet |
| F | P1-SESSIONS | Partial | None yet |
| G | P1-ENTITLEMENT | Partial | None yet |
| H | P2-BILLING | Missing | None yet |
| I | P2-RESELLER | Missing | None yet |
| J | P2-EVENTS | Missing | None yet |
| K | P2-PORTAL | Partial | None yet |
| L | P1-IMPORT | Partial | None yet |
| M | P2-REPORTS, P3-LOAD | Missing | None yet |
| N | P2-SETUP | Partial | None yet |

Missing-only axes H/I/J/M remain Missing in FEATURE-INVENTORY. C hardware state is Implemented but Not Hardware Verified. No global completion ratio is asserted.

Narrow accepted implementation: P0-INPUT (#77) preserves decimal strings and rejects incomplete commands. All existing source regressions/full local/current-base PR CI passed at bdc31ae3. Merged 149123b7; post-merge CI pending. This does not complete axis A/B/C.
