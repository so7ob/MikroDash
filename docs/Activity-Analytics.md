# Activity analytics

`/activity` reads structured `DNSUSER` and firewall records from RouterOS disk logs.
Enable synchronization per router, choose its log timezone and folder (default
`usb1-part1/Log`). The API account needs access to `/file/read`. The documentation lists
`read,api,ftp,test`, but RouterOS 7.24.5 was observed rejecting file reads even
with those policies and `!write`; do not infer successful access from the policy
list alone. Verify the actual command and surface a permission trap.
No RouterOS configuration or file writes are issued. The existing `/logs` view
continues to display the live memory log.

A held session supplies the existing shared RouterOS connection. Activity's hold
runs no page collectors: the archive worker lists files every five seconds and
uses `/file/read` in chunks of at most 32768 bytes. Only `.txt` children of the
configured folder are considered. Each router is processed serially with a 2 MiB
read budget per pass. Nothing is downloaded from third parties.

Unchanged complete files are skipped by their manifest. Changed or renamed files
are matched to stored content prefixes and the last committed boundary. Generation
plus byte offset identifies a source row, retaining independent identical lines.
Every batch commits its events, invalid-row counter and checkpoint in one SQLite
transaction. Partial final lines wait for a later pass. A failed read or database
write leaves the checkpoint unchanged. Prefix and chunk verification detect files
rotating during a read. Checkpoints survive restarts and are independent of the
rotating filename. Byte-for-byte identical archived copies represent the same
source content; no file format can distinguish such copies from independent
files with exactly the same bytes.

Only normalized DNSUSER and firewall metadata is stored, never unrelated raw
log text or credentials. Records older than 30 days are not imported; the hourly
activity-only retention sweep expires imported records after 30 days. Archive
files and existing application tables are untouched. Router time and reception
time are distinct. Unknown timestamps are marked uncertain, with reception as
the query time. Logs must use the timezone configured for that router.

Rules are scoped to the selected router. Local overrides precede built-ins;
the longest matching domain wins. Stored events are classified at query time,
so an edit applies to the retained history immediately. DNS counts describe
recorded queries, not confirmed visits or usage volume. Firewall packet lengths
are metadata, not traffic totals, and IP destinations are never guessed to be
domains by proximity to DNS events.

REST endpoints under `/api/activity/` share router-scoped page authorization.
Read permission covers queries and CSV, write permission covers source settings
and rules. CSV escapes spreadsheet formula cells. WebSocket invalidations are
authorized for each principal at send time, and the page re-queries SQLite with
its current URL filters. API/file content is never sent on that notification.

Disk logs are rotating files, not an unlimited archive. If RouterOS overwrites a
file before MikroDash reads it, those missing rows cannot be recovered from it.
Sync status displays failures, last successful scan and invalid records.

## Visual analysis and geography

Overview includes service, category, user and destination-country doughnuts,
a time histogram, rankings and a weekday/hour heatmap. Grouped analysis offers
service, domain, country, category, client, day and hour using the same URL filters
and event drilldowns. On the Activity overview, clicking a chart label, ranking
or time bar filters the current view; Ctrl + click opens the matching Events tab.
Country distribution counts Firewall records; no country is
inferred for DNSUSER records without an explicit destination IP. The shared local
GeoIP database supplies approximate destination countries/cities. Unknown IPs or
a missing database remain Unknown. Labels and CSV retain that distinction.

## Optional dashboard cards

On `/home`, choose Edit → Add Card and select Activity Overview, Activity Timeline,
Activity Services, Activity Users, Activity Categories or Activity Countries. They
start hidden, so adding them preserves existing saved arrangements. Save, Discard,
remove, drag and resize use the existing dashboard editor and per-user persistence.

All six summarize the selected router's last 24 hours. One shared request to
`/api/activity/dashboard` feeds them; visible cards refresh on `activity:changed`
and on returning to the Dashboard. Hiding the cards/page cancels pending work.
No collector or browser polling loop is added. Drilldowns carry the router and
matching filter into `/activity`. The dashboard endpoint checks both Dashboard
and Activity read permission on that router. The card picker follows the source
page's permission, and permission changes clear displayed Activity data.

## Verification

Run `sh tools/verify.sh` for all repository checks. The optional browser journey
uses an isolated SQLite database and a synthetic RouterOS archive, never live
hardware. With Playwright and @axe-core/playwright available to Node, run:

```sh
NODE_PATH=/usr/lib/node_modules MIKRODASH_ACTIVITY_BROWSER=1 \
  go test ./internal/server -run TestActivityBrowserJourney -v -timeout 90s
```

The same browser command with `-run TestActivityDashboardBrowserJourney` verifies
the optional cards through the real editor and APIs, including layout portability,
resize, drilldown, invalidation, permission changes and router switching. Card
screenshots are written under `/tmp/mikrodash-activity-cards-*.png`.

The journey verifies import, filters, CSV, editable classification, country/day/hour
groups, four doughnuts, URL restoration, keyboard, 375/1280 and light/dark modes.
Screenshots are written under `/tmp/mikrodash-activity-*.png`; accessibility failures
fail the journey.
