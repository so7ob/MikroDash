# Subscriber Management UI and navigation unification

Date: 2026-11-13, Asia/Aden. Parent platform #38; scoped implementation #97.
Baseline: develop `404622b3`. This extends the existing 229 A-N acceptance
checkpoints and P0–P3 roadmap. It does not restart the platform or close #38/#76.

## Reference and observed differences

Read before implementation: root AGENTS.md, CLAUDE.md, CONTRIBUTING.md, DESIGN.md;
`web/src/pages/interfaces.ts`, `vlans.ts`, `bridges.ts`, their actual page templates,
Dashboard markup, `resource.ts`, `modals.ts`, `form.ts`, `dom.ts`, `nav.ts`,
`routing.ts`, `caps.ts`, and `web/public/app.css`.

The old subscriber screen combined five unrelated administrative tabs, permanent
account and plan forms, search, permanent bulk-change fields, and a table. Plan
fields preceded published versions. Assign/renew used browser prompts;
reconciliation used a browser confirmation. There was no independent overview,
no direct address for each function, no compact summaries, and no bounded DOM
pagination. Network already separates list browsing from resource dialogs.

The implemented foundation adopts Network card furniture, not a second design
system. Account usernames and display names remain original Unicode; new UI stays
English/LTR. Old `/subscribers` links still open the subscriber directory.
The module's first sidebar child/default entry is `/subscriber-overview`.

## Reused design system

| Concern | Existing source and reuse |
|---|---|
| Tokens/themes | app.css `--bg-deep`, `--bg-card`, `--border`, `--text-main`, `--text-muted`, accent roles; appearance/light/Nord layers stay authoritative |
| Typography | `--font-ui`, `--font-mono`, existing card/table/label scales; no new font or library |
| Spacing/shapes | DESIGN ladder .25/.5/.65/.75/1/1.1rem; existing 4/6/10/12px shapes |
| KPI cards | VLAN/Bridge `.wl-summary-row`, `.wl-summary-card`, `.wl-summary-title`, `.fa-stat-val` |
| List furniture | `.card`, `.card-header`, `.card-title`, `.card-badge`, `.hdr-actions` |
| Tables/sort | `.table.table-vcenter`, `.data-scroll`, `renderSortHeader`, `sortRows`; existing table density tokens |
| Forms | `fieldHtml`, `.ff-grid`, `.ff`, `.sform-label`, `.sform-input`, `.ff-help`, `toggleHtml` |
| Buttons/icons | `.sbtn` semantic variants, `.is-sm`, existing row sizing; inline line SVG, unique nav shapes |
| Status | `pill` and palette tones, labels as well as colour |
| Modals | Extend shared modals.ts with `managedModal`; same `.rtr-modal-bg`, `.rtr-modal`, header/body/footer and compact/wide variants as resource dialogs |
| Modal lifecycle | Dirty-close review, Escape/backdrop, first focus/trap/return, inert background, busy fence, retained failed fields, reusable destruction for refreshed cases |
| Sidebar | Actual shell `.nav-group`, header/chevron/body/nav item, nav.ts disclosure and auto-expansion; existing overflow and mobile drawer |
| Routing/authority | internal/pages registry and generators; routing.ts URL/history; original global `system:subscribers`, live server check per API/page request |
| Charts | Real account-status distribution uses labelled progress bars and counts; existing snapshot quota cards retained; no artificial trend samples |
| Empty/loading/error | Honest list empty rows, snapshot loading, preserved server failure text, runtime disclosure and explicit Not measured |

## Implemented page acceptance and remaining functional dependencies

The shared foundation below has passed the final browser matrix. Broader functional
closure remains partial where a dependency is explicitly listed. Full repository
and protected delivery evidence is recorded against the final head in Issue #97.

| Page | Implemented foundation | Remaining acceptance/dependency | State |
|---|---|---|---|
| Overview | Default child route, real total/active/suspended directory counts, status distribution, configured runtime, unavailable metrics labelled | Aggregate confirmed usage, subscription expiry/pending/online counts and real trends need scoped SQL reporting | Foundation browser-verified; full workflow partial |
| Subscribers | List first, Add modal, fenced display-name editor, prompt-free plan assignment, selection-only bulk controls and frozen review modal, search/status/sort and 50-row DOM pages; read-only detail for Profile/Subscription/Quotas/Sessions/Devices/Accounting plus status-history link | P1-DIRECTORY server pagination/plan filter, plan/usage/remaining/last-seen projection and complete general account audit; no fabricated columns | Foundation browser-verified; full workflow partial |
| Plans & Policies | Published-version list, search/sort, compact summaries, grouped wide Add/New version editor, exact units, structured add/edit/remove limits | Full saved-policy states, explicit speed-profile editing, schedule windows, copy/archive and subscriber assignment distribution remain P0-PLAN | Foundation browser-verified; full workflow partial |
| Policy Simulator | Account/time controls, Run, current vs assumed-time decision, Allowed/Denied reasons, existing quota visualization, recorded sessions count and accounting quality | Arbitrary draft-plan scenarios and richer timeline comparison remain functional work | Foundation browser-verified; full workflow partial |
| Status History | Separate page, account selector, table, actors/reasons/exact operation IDs, loaded-event KPIs, date/action filters, evidence/details dialog | Global server date filters, general account audit and reliably documented legacy before-state remain P0-HISTORY; never invent unknown transitions | Foundation browser-verified; full workflow partial |
| Reconciliation | Account-scoped real category KPIs, case/evidence list, independent review dialogs, explicit confirmation, retained invalid input, asynchronous account-race fence and refresh after commit | Global queue/aggregation and broader admin accounting reports remain roadmap work | Foundation browser-verified; full workflow partial |

Plan versions are never overwritten. New version opens a detached copy and
increments the known highest version; server constraints remain authoritative
against concurrent publication. Other policies, calendar windows and saved speed
profiles are retained. Existing subscriptions keep their assigned version.
Amounts use decimal strings and BigInt, SI rates and IEC data units. Fractional
base units and signed-64-bit overflow are refused instead of rounded.

The simulator reads the actual snapshot evaluator at a validated RFC3339 time.
Its assumed time does not rewrite usage, subscription dates, leases or routers.
A recorded lease count is not an online subscriber count.

Reconciliation counts describe the loaded account and bounded categories (up to
1,000 each), not the installation. Byte distribution remains an explicitly
approved allocation, not a measured traffic history. Missing/malformed statistics
and held/provisional balances keep their original meanings.

## Target sidebar and future-page acceptance

Implemented now: Overview, Subscribers, Plans & Policies, Policy Simulator,
Status History, Reconciliation. The latter two specialist pages preserve existing
functionality while moving it out of module-level horizontal tabs.

Do not expose the following target children until their protected backend and
complete workflows exist. Every row inherits list-first, shared furniture,
permission/deep-link/mobile/keyboard/theme/Unicode/failure acceptance.

| Target child | Required list/visual/action contract | Dependency/status |
|---|---|---|
| Active Sessions | Real freshness/rate/accounting summaries, bounded live table, guarded detail/disconnect review | P1-SESSIONS, pending |
| Vouchers & Batches | Batch KPIs/inventory, issue dialog, reviewed preview/export/print, recoverable delivery | New scoped voucher issue; #94 closed not_planned |
| Subscriptions & Renewals | History/list, fresh-period/no-rollover balance review, idempotent operation receipt | P0-COMMERCIAL/P1-ENTITLEMENT, pending |
| NAS & Networks | Device/site cards, truthful readiness/connection diagram, secret-write-only configuration dialogs | P2-SETUP and hardware evidence, pending |
| PPPoE Management | Independent PPPoE sessions/admission/attributes/runtime view and guarded operations | P1-PPPOE, pending |
| Resellers | Ownership-scoped stock/accounts/limits, list/details/action dialogs | P2-RESELLER, pending |
| Billing & Payments | Exact financial summaries, real trends, invoices/payment lists, immutable receipt/reversal dialogs | P2-BILLING, pending |
| Usage & Accounting | Confirmed vs estimated/held readings, real period charts, accounting/evidence table | P0 accounting/P2-REPORTS, pending |
| Reports & Analytics | Date/site/plan filters, bounded aggregate charts and safe exports | P2-REPORTS, pending |
| Notifications | Real outbox/delivery states, channel/list/detail/retry dialogs | P2-EVENTS, pending |
| Import & Migration | Read-only source preview then reviewed mapping/staging/apply/resume/rollback wizard | #79 preview retained; P1-IMPORT apply pending |
| Settings | Readiness and configuration cards; reviewed saves/backup/restore; no activation bypass | P2-SETUP, pending |

Sidebar disclosure uses the same Network mechanism, including session-only
expansion, grouped/flat preference, collapsed rail and mobile drawer. A navigation
into a child auto-opens its tree. Existing global authority is deliberately
preserved; new page-only grants cannot elevate a viewer to subscriber admin.
Requests re-check server permission, independent of sidebar visibility.

## Implementation order

1. Verify current checkout, preserved branches/work and open PRs; create #97 from
   current develop. Add UX01–UX09 to the feature matrix without deleting A–N.
2. Shared sidebar/routes/authority and modal lifecycle first; existing account,
   plan, status and reconciliation UI next. Preserve backend contracts.
3. Browser/visual correction rounds, exact arithmetic/permission/readonly tests,
   full stable verification and protected PR/develop delivery.
4. Resume P0-CREDENTIAL immediately using the same modal pattern; preserve the
   original credential-epoch/session/ticket/RADIUS race acceptance. Then commercial
   idempotency and complete saved-policy editors. Add matching UI acceptance to
   each subsequent P1/P2/P3 increment; never build placeholder forms ahead of it.

## Evidence and before/after

Baseline captured before edits by tools/subscriber-plan-browser.mjs against
`404622b3` frontend with synthetic HTTP: `/tmp/mikrodash-97-before/`. Final
artifacts, commands, tested SHA, screenshot comparisons and any incomplete
acceptance are recorded here after testing. No router/runtime was enabled.

Current retained setup/test corrections: compact runtime details must be opened
to read their explanation; overlay geometry mutations must target the visible
modal control, since the underlying search control is intentionally inert.
The repository's original static gates identified missing new catalogue entries,
field help nodes and literal unit producers; these were corrected without
weakening checks. Ignored operator `.temp/geo/cities.json` exceeds the credential
scanner's 8MB limit; final full source verification uses an isolated clean
worktree while preserving that file in the visible checkout.

Shared accessibility corrections also retain the established design: secondary buttons and field labels use readable existing text tokens, table headers follow the existing accessible palette rule, and the light warning token is darker after measured pill contrast failures. Long managed dialogs now keep header and footer visible while their body scrolls.


### Final local browser evidence

| Check | Command / result | Meaning |
|---|---|---|
| Protected administration | `SUBSCRIBER_BROWSER_MODULE=/tmp/mikrodash-88-browser SUBSCRIBER_EVIDENCE_DIR=/tmp/mikrodash-97-final-sqlite-evidence go test -tags=subscriberbrowser ./internal/server -run '^TestSubscriberAdminReconciliationBrowserAcceptance$' -count=1 -timeout=10m` — PASS, 286.528s | Actual HTTP handlers/SQLite; create/cancel/validation/stale-name fencing, frozen atomic status batches/ABA conflict, readable exact audit history, malformed receipt/byte conservation refusal, retained evidence, committed settlement counters, account-response race, snapshot detail, sidebar disclosure/reload/back/forward/active/mobile/short scroll, dirty Escape/focus and 5,001-row synthetic directory rendering |
| Plan editor | `tools/subscriber-plan-browser.mjs` with the same external module and `/tmp/mikrodash-97-plan-evidence` — PASS, 12 appearance/viewport axe cases | Synthetic HTTP; add/edit/cancel/remove draft limits, exact >2^53 amounts, pending/repeated/disconnected/refused save, retained draft, immutable New version copy; dark/light/Nord at 375/768/1280/1920 |
| History controller/browser | `tools/subscriber-history-browser.mjs` with `/tmp/mikrodash-97-history-evidence` — PASS, 8 appearance/viewport axe cases | Synthetic HTTP; 401/403, stale success/rejection isolation, account-switch race, partial JSON/retry, exact cursors, escaped data, final-page keyboard focus |
| Contracts | Focused server/RBAC tests — PASS | Direct page denial, existing global authority with zero routers, no page-grant escalation, validated read-only simulation time and unchanged durable subscription/usage |
| Frontend | `npm run typecheck`, `npm test` — PASS, 167 tests | Current TypeScript source and existing regressions retained |
| Shared visual rules | Existing form/icon/nav/selector/native-widget static gates — PASS | No weakened static check or duplicate component system |

The protected browser audits all six module pages in dark/light at all four
widths, plus existing dialog cases in dark/light/Nord. Axe reports zero findings
in the completed runs. This is automated accessibility plus keyboard/focus proof,
not a claim of universal WCAG certification. NAS traffic is synthetic. The final
5,001-row directory test intercepts one GET and proves bounded DOM rendering and
Unicode search; it does not prove server-side pagination or 100k platform scale.

The final ordinary repository gate, tested SHA, PR CI, merge SHA and post-merge CI
are recorded in Issue #97 to avoid self-referencing the documentation commit.
The full design task stays open for its named backend/UI dependencies; this PR
ships a verified foundation rather than declaring the complete platform finished.

### Committed before/after images

Baseline captures use an isolated worktree at `404622b3`, its original CSS and
actual page module with synthetic HTTP. Final administration captures use the
protected HTTP/SQLite fixture. Network reference captures use the real templates
with unavailable measurements, not a live router. Source/image SHA-256 hashes and
dimensions are in [the manifest](ui-evidence/manifest.json).

| Surface | Before | After |
|---|---|---|
| Subscribers 1280 | [Permanent forms](ui-evidence/before-subscribers-1280.png) | [List first](ui-evidence/after-subscribers-dark-1280.png) |
| Subscribers 375 | [Permanent forms](ui-evidence/before-subscribers-375.png) | [Mobile light list](ui-evidence/unified-subscribers-light-375.png) |
| Plans 1280 | [Long inline editor](ui-evidence/before-plans-1280.png) | [Published list](ui-evidence/after-subscriber-plans-dark-1280.png), [modal](ui-evidence/after-plan-modal-dark-1280.png) |
| Plans 375 | [Inline fields](ui-evidence/before-plans-375.png) | [Scrolling modal with fixed context/actions](ui-evidence/after-plan-modal-dark-375.png) |
| Sidebar | [Single module entry](ui-evidence/before-sidebar-1280.png) | [Expanded category](ui-evidence/sidebar-expanded-dark-1280.png), [short mobile drawer](ui-evidence/sidebar-mobile-dark-375.png) |

Additional final frames: [Overview](ui-evidence/after-subscriber-overview-dark-1280.png),
[Simulator](ui-evidence/after-subscriber-simulator-dark-1280.png),
[History](ui-evidence/after-subscriber-history-dark-1280.png),
[Reconciliation](ui-evidence/after-subscriber-reconciliation-dark-1280.png),
[name dialog](ui-evidence/subscriber-name-editor-synthetic-dark-default-375.png),
[approval dialog](ui-evidence/subscriber-admin-reconciliation-synthetic-light-default-375.png).
References: [Interfaces](ui-evidence/reference-interfaces-dark-1280.png),
[VLANs](ui-evidence/reference-vlans-dark-1280.png),
[Bridges](ui-evidence/reference-bridges-dark-1280.png).

Visual correction rounds fixed measured secondary-action/label/header/light
warning contrast, visible-control geometry mutation, modal footer placement and
long-dialog header/action visibility. Remaining functional dependencies in the
page matrix are intentionally still partial; future children are not exposed.
