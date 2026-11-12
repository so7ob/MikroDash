# Activity Analytics performance verification — Issue #82

Measured locally on 2026-11-05. These are development verification results, not
proof of deployment or a universal latency guarantee for every data distribution.

## Real HTTP acceptance

The opt-in Go test exercises the authenticated `/api/activity/query` handler through
an actual TCP HTTP server, including JSON transfer and decoding. Each case uses
20 samples per cache state; p95 is the nineteenth sorted sample. The fixtures have
135,000 and 1,000,000 independently identified observations spread over 29 days,
95% DNS / 5% firewall, two observations per displayed group, 2,000 domain slots,
100 user/device names (including Arabic), 200 source addresses and roughly 1,910
distinct normalized shapes. No RouterOS connection is opened.

Test host: four virtual CPUs, Intel Xeon E-2176M @ 2.70GHz, approximately 16 GB RAM,
local block storage, Linux, Go 1.27.3 and `GOMAXPROCS=2`. Other local services
remained running. Timings are milliseconds.

| Case | 135k cold | 135k warm | 1m cold | 1m warm |
| --- | ---: | ---: | ---: | ---: |
| overview | 377.8 | 2.6 | 722.2 | 2.2 |
| search | 180.3 | 4.4 | 611.7 | 1.7 |
| filter | 21.9 | 3.5 | 13.7 | 2.1 |
| combined | 15.4 | 2.9 | 16.5 | 1.9 |
| sort | 16.9 | 3.6 | 14.0 | 2.3 |
| sort-label | 46.8 | 4.0 | 167.9 | 2.2 |
| sort-user | 54.0 | 2.4 | 176.8 | 2.2 |
| cursor | 23.5 | 3.7 | 13.3 | 2.4 |
| deep-page | 45.9 | 3.2 | 22.2 | 2.6 |
| during-import | 147.6 | — | 126.1 | — |

All measured cases meet the acceptance thresholds: p95 ≤500ms for Events
navigation/structured filters/sorts, and ≤2s for overview/substring search.

**Cold** means a miss in the bounded result cache. It does not mean a dropped OS
page cache or a new SQLite connection. Event totals may reuse the separately keyed
counts cache when only the page or ordering changes; this is the normal navigation
path. **Warm** repeats the identical query immediately. Overview cold samples vary
the legacy page parameter, search uses an Arabic substring, combined filters use
domain/device/query type, and deep-page exercises legacy pages 1001–1020. Cursor
samples follow returned forward cursors. Initial projection preparation is excluded
from steady-state percentiles: preparation plus the first complete million-event
HTTP result took 1.110s after seeding/indexing. Fixture generation itself took about
7m13s at one million observations and is not an import-throughput guarantee.

During-import measurements use a separate writer committing batches of 100 new
observations while the HTTP client follows cursors. Each size observed 20 distinct
data revisions and 2,100 committed new observations. Tests assert the final exact
retained count, zero pending facts and no duplicate group IDs in each returned page.
There is no warm value because each import invalidates results by revision.

Machine-readable results: [activity-82-http.json](verification/activity-82-http.json).
Reproduce both sizes with:

```sh
MIKRODASH_ACTIVITY_PERF=1 MIKRODASH_ACTIVITY_PERF_REPORT=/tmp/activity-http.json GOMAXPROCS=2 go test -p 2 ./internal/server   -run '^TestActivityHTTPPerformance$' -count=1 -v -timeout=30m
```

## Isolated upgrade and distribution limits

A read-only SQLite backup of the local operator database was migrated from schema
44 to 45 in a separate directory. Its 136,167 source observations became 136,167
indexed facts in 53.864s, without reading router archives. Full legacy-versus-indexed
result comparison passed using the actual local GeoIP data. A second process resumed
with zero pending rows and reused persisted derived fields. No operator identities,
addresses, event payloads or GeoIP files are included in this report or fixtures.

This copy has 24,244 distinct shapes and 128,650 displayed groups, substantially
more variety than the repeated-domain synthetic fixture. Direct query measurements
(SQL plus application processing, **not HTTP p95**) after derivation were:

| Query | Elapsed |
| --- | ---: |
| Last 24h overview | 313ms |
| Last 24h Events | 134ms |
| Last 30d Events | 222ms |
| Full legacy 30d analysis | 4.12–5.01s |
| Full indexed 30d analysis, result-cache miss | 2.32–2.93s |

The first indexed 30d result including all derived-field preparation took 10.76s.
Thus the synthetic HTTP acceptance passed, while a cold full 30d overview on this
higher-variety copy still exceeds 2s. Distinct shapes, answer multiplicity, timezone
and filter selectivity affect cost; one million repeated observations is not evidence
for one million unique shapes. Keep this limitation visible when planning activation.

## Correctness and interaction checks

- Indexed-versus-legacy parity covers summary/ranking totals, previous period,
  literal Unicode/`%`/`_` substrings, country-answer units, source types, exact time
  boundaries, fractional offsets and DST, all event sort directions and CSV.
- Schema-44 upgrade, interrupted batches, restart/resume, complete fallback during
  backfill, replay enrichment, unchanged scans, rotation and genuine duplicates
  are covered. Injected projection failure rolls back events and checkpoints.
- Cache tests cover bounded size, immutable responses, imports, rules, timezone,
  GeoIP, rolling-window expiry and future-dated events. Permissions are checked
  afresh at the HTTP boundary; canceled queries can leave the serialization queue.
- Browser tests cover actual arrivals during navigation, later-page anchors,
  focus/scroll restoration, coalescing, filter cancellation, denied-router changes,
  status-only notifications and visible-tab-only requests/rendering. Tests run at
  375/1280 widths with keyboard navigation, Ctrl-copy (clipboard and fallback),
  dark/light/Nord themes, and zero axe violations in the tested states.
- Focused Go race checks passed for indexed cursor/cache/retention, wire status
  and HTTP permissions/partial responses.
- Four compiling mutations were killed by behavioral assertions: ignoring data
  revision in cache keys, suppressing duplicate group increments, counting shapes
  instead of observations in rankings, and invalidating analysis on every unchanged
  archive scan. Mutations were restored before final verification.

Reproduce browser checks with:

```sh
MIKRODASH_ACTIVITY_BROWSER=1 GOMAXPROCS=2 go test -p 2 ./internal/server   -run '^TestActivity(BrowserJourney|DashboardBrowserJourney)$'   -count=1 -v -timeout=10m
```

The full repository gate and GitHub CI evidence for the final revision are recorded
in the delivery PR. Browser/performance tests are opt-in and are not implied by an
ordinary `go test ./...` run.

## Activation boundary

SQLite and the existing 30-day retention policy remain in place. Original events
remain authoritative and projections are transactional and resumable. No archive
was deleted, no router configuration was changed, and the upgraded binary was not
started against the operator database. Activation is a separate step after review.
The unfinished Issue #76 branch remains preserved for later work.
