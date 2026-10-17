# Activity analytics

`/activity` reads structured `DNSUSER` and firewall records from RouterOS disk logs.
Open the **Synchronization** tab to see archive status and enable synchronization
per router, then choose its log timezone and folder (default
`usb1-part1/Log`). The API account needs access to `/file/read`. The documentation lists
`read,api,ftp,test`, but RouterOS 7.24.5 was observed rejecting file reads even
with those policies and `!write`; do not infer successful access from the policy
list alone. Verify the actual command and surface a permission trap.
No RouterOS configuration or file writes are issued. The existing `/logs` view
continues to display the live memory log.

Synchronization status and settings appear only in their own tab. Tab selection
is retained in the URL and supports arrow keys, Home and End. Router selection
and analysis filters are preserved when switching views. Selected filters use
colored chips by filter type; click one to remove it, or use **Clear** to remove
all selected drilldown filters.

## Router setup from Configuration Library

Open **Config Management > Library > Monitoring > Activity analytics (DNSUSER + USB)**.
Its Preview contains the complete `DNSUserLine` script and logging configuration,
with the USB setup checklist at the start of the template source. It is based on a working RouterOS
**7.24.5** setup, inspected read-only. The router must support logging actions
with `target=script`; check that capability before using it on another release.
No scheduler is needed: the logging action supplies the original `$message`.

### Prepare the USB drive

1. Attach a USB flash drive or external drive to the **MikroTik router**. Confirm
   that RouterOS recognizes and mounts it in **System > Disks** and **Files**.
   Use the actual mounted name; `usb1-part1` is only the example/default.
2. In **Files**, create a `Log` directory inside that mounted drive. The directory
   must exist before the disk logging action is configured. The template does
   not partition, format, mount or erase storage, or fall back to internal flash.
3. Set **USB log filename** to `usb1-part1/Log/netlog`, or your mounted directory
   followed by `/netlog`. Do **not** append `.0.txt`: RouterOS creates
   `netlog.0.txt`, `netlog.1.txt`, and so on itself.
4. Choose capacity for your event rate and longest expected MikroDash outage.
   Defaults match the reference setup: **1,000 lines per file and 1,000 files**,
   with rotation enabled (`disk-stop-on-full=no`). These are line limits, not a
   duration or a fixed byte size. Watch the drive's free space and write load.
   Once the oldest file is overwritten, its unsynchronized rows are unavailable.

### What the template configures

| Component | Settings and purpose |
| --- | --- |
| `/system script`: `DNSUserLine` | Parses the incoming DNS query; snapshots HotSpot user/server, DHCP/ARP MAC and device name; writes structured `DNSUSER` JSON. Manual MAC-to-user/device maps start empty. |
| Logging action `DNSJoin` | `target=script`, `script=DNSUserLine`. |
| Logging action `NetArchive` | `target=disk`, the chosen USB filename and rotation limits. |
| DNS rule | `topics=dns`, `regex="query from [0-9.]+: #"`, `action=DNSJoin`. |
| DNSUSER rule | `topics=script`, `regex="^DNSUSER"`, `action=NetArchive`. |
| HotSpot archive rule | `topics=script,info`, `regex="^HS_"`, `action=NetArchive`. It preserves messages from existing HotSpot hooks; this template does not create hooks, and Activity does not count `HS_` as DNS events. |

The script reads cached A/AAAA answers and follows up to five CNAME hops, preserving
`canonical`, `dns_ips`, `ip_basis`, and `device_name` alongside the basic fields.
The provided DNS rule/script captures **IPv4 query sources**; cached answers can
include **IPv4 and IPv6**. It does not claim coverage of IPv6 client queries.
A cache miss can leave `dns_ips` empty: the query is logged before its answer is
necessarily cached. The optional synchronous resolver fallback stays disabled.
RouterOS skips matching log messages while a logging script is busy, so these
are recorded observations, not a complete count of all network DNS traffic.

Clients must already use this router as their resolver. LAN DNS access and WAN
protection must already be correct. The template does not enable DNS service,
redirect queries, change DHCP/firewall settings, or reveal DNS sent directly to
other resolvers or encrypted inside client DoH/DoT. Firewall event logging is
optional; existing firewall records in the archive are parsed separately.

### Review, deploy and connect Activity

1. Choose **Preview** in the library and review the instructions, script and three
   settings. The source is a MikroDash template: its `ensure` commands are
   resolved by the deployment engine, not RouterOS console commands to paste.
2. For an initial setup, use **Deploy**, select the router, inspect its rendered
   preview, acknowledge the existing script/code findings and confirm the target.
   The standard backup, syntax checks and deployment guards still apply.
   The template sets the existing `DNSUserLine` source and
   `policy=read,write,test dont-require-permissions=yes`, allowing the logging hook
   to invoke it. This does not grant rights to MikroDash's API account. Preserve
   any manual labels before replacing an existing script. A working setup does
   **not** need to be re-applied just to enable Activity.
3. Named actions and exact rules are reused. No archive files or unrelated rules
   are deleted, and repeated application does not add a second matching rule.
   If you already have extra overlapping rules, review them to avoid duplicate
   log delivery. A customised clone retains the existing stricter custom-script
   guard; cloning does not confer shipped-template trust. Optional manual MAC
   labels can be maintained in the router's script with your normal admin tools.
4. Query a domain from a LAN client using this router's DNS. Check for
   `DNSUSER` in **Logs** and an increasing `netlog.0.txt` under the USB folder
   in **Files**. Running `DNSUserLine` by hand provides no query `$message`.
5. Open **Activity > Synchronization**, select the router, and explicitly enable
   synchronization. Enter **the folder only** (`usb1-part1/Log`, without
   `/netlog`) and the router log timezone. Save, then check the last successful
   synchronization, files, invalid rows and any read failures. Verify actual
   `/file/read` access as described above; a group policy list alone is not proof.

MikroDash incrementally reads the files into SQLite; analysis then reads SQLite.
It resumes from persisted checkpoints after restart/reconnect, imports available
new records while the page is closed, and retains **30 days** in its database.
This does not change USB rotation limits or preserve records that RouterOS has
already overwritten. Disabling synchronization stops collection for that router.

MikroTik references: [logging and script hooks](https://manual.mikrotik.com/docs/diagnostics-monitoring-and-troubleshooting/log/),
[logging action fields and USB rotation](https://manual.mikrotik.com/docs/cli-reference/system/logging/action/),
[DNS cache records](https://manual.mikrotik.com/docs/cli-reference/ip/dns/cache/all/),
and [script permissions](https://manual.mikrotik.com/docs/developer-guides/scripting/scripting-tips-and-tricks/).

## Incremental collection and storage

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
and event drilldowns. On the Activity overview, clicking a doughnut slice, chart
label or ranking toggles its filter in the current view; selecting it again
removes only that filter. Legend buttons expose their selected state and provide
the same action with Enter or Space. Other is an aggregate with no single filter.
Time bars also toggle: a selected period is one pressed bar; clicking it again
restores the preceding period (including custom endpoints), preserving other
filters and surviving URL reload. Choosing a period manually starts a new
selection. Ctrl + click opens matching Events even when the value is already
selected. Removing a country selection restores the prior event
source, including after reloading a shared URL.
The country panel defaults to DNS answer countries; its Location source selector
switches to explicit Firewall destinations. The URL retains the selection. DNS
country slices count answer observations (one address once per event), while
Firewall slices count events. One DNSUSER event may supply several answers in
several countries. Selecting a DNS country filters events containing at least one
answer in that country; the distribution still describes all answers in the
matching events. Firewall country filters only match Firewall events. Grouped
analysis, removable filters, Ctrl Events drilldowns and CSV preserve both sources.

The shared local GeoIP database supplies approximate countries/cities. DNS answers
show their own country and city in Events and in the `dns_answer_locations` CSV
column; they never populate the connected destination. Unknown addresses remain
an Unknown bucket. Without a database, recorded answer IPs still appear and their
country distribution remains Unknown, with an explicit setup status. This is an
IP location estimate, including CDN/anycast infrastructure, not a service's home
country, a user's location or evidence that an answer was actually contacted.

### GeoIP for local runs

The standard Docker image downloads DB-IP City Lite at image build and includes
`/app/geo/dbip.mmdb`. A plain Go build does not include the data. Pass `-geo` a
local directory containing `dbip.mmdb` (DB-IP City Lite), or the compatible legacy
`.dat` files. The database is loaded once at startup; placing a file after startup
requires restarting the application. For example, after obtaining and decompressing
the City Lite database into `./devdata/geo/dbip.mmdb`:

```sh
./mikrodash -listen 127.0.0.1:3082 -data ./devdata \
  -web web/dist -static web/public -geo ./devdata/geo
```

The Activity setup message reports missing data rather than hiding IP records.
Geolocation itself performs no network requests and sends no user addresses to
external lookup services. Database installation/restarting an operator's running
server is an operational step separate from delivery into `develop`.

## Enriched DNSUSER evidence

DNSUSER may also supply `Destination.canonical`, `Destination.dns_ips`,
`Destination.ip_basis` and `Source.device_name`. Events retain and show these
fields with the original requested domain, query type, matching method,
server, user, IP and MAC. Both IPv4 and IPv6 answers are retained, including when
an A query's record includes both families. These answers never become a
connected destination; their locally derived countries remain separately labelled DNS evidence. Classification uses the requested
domain; a device name never replaces historical user identity.

Overview adds answered-event, unique-answer-address, device-name and
canonical-name counts, a sortable/paginated evidence table (10 rows by default),
and query-type, matching-method and IP-basis distributions. Choose addresses,
device names, canonical names, requested domains or hotspot servers in the table.
Grouped analysis also offers the new dimensions. Each answer address counts once
per DNSUSER event, so a record can contribute to several address rows; totals
still count the record once. Missing legacy metadata is shown as unavailable.
The new dimensions use the same URL filters, repeated-selection toggles and Ctrl
Events drilldowns; search and CSV include all four new fields.

On the first enabled synchronization after this parser upgrade, existing file
generations are revisited once in bounded chunks. Only retained rows with the
same generation/byte identity receive metadata updates. Event time, reception,
user identity and event counts are preserved; pruned/missing rows are not
reinserted and old invalid-line counters are not recounted. Upgrade progress and
metadata commit together, so an interruption resumes at the saved byte offset.
Unchanged files then return to the normal incremental path, including after
restart. Data no longer present in USB archives cannot be recovered, and messages
that never supplied these fields remain unavailable. No schema reset, router
configuration change or external DNS/classification service is involved.

## Optional dashboard cards

On `/home`, choose Edit → Add Card and select Activity Overview, Activity Timeline,
Activity Services, Activity Users, Activity Categories or Activity Countries. They
start hidden, so adding them preserves existing saved arrangements. Save, Discard,
remove, drag and resize use the existing dashboard editor and per-user persistence.

The Countries card prefers recorded DNS answers when available, with answers as
its unit and DNS-country drilldowns. Otherwise it uses explicit Firewall events.

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

The journey verifies the synchronization tab's isolation, URL/keyboard navigation,
read-only source controls, colored filters and Clear, import, CSV, editable classification, country/day/hour
groups, DNS/Firewall country units and missing-database setup, seven doughnuts, recorded DNS/device evidence, paginated/sorted evidence
groups, repeated-selection toggles, painted arc targets (including
sub-1% and full-circle slices), touch, Ctrl drilldowns, Other, URL/source restoration,
keyboard, 375/1280 and light/dark modes.
Screenshots are written under `/tmp/mikrodash-activity-*.png`; accessibility failures
fail the journey.
