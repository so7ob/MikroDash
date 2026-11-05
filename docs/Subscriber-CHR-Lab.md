# Isolated RouterOS CHR protocol lab

Issue [#54](https://github.com/so7ob/MikroDash/issues/54) continues the open
[completion contract](Subscriber-Completion.md). This is an x86 CHR software lab,
not L009 hardware acceptance or authorization to change a live router.

## Ownership boundary

`tools/subscriber-chr-lab.mjs` starts one disposable CHR 7.24.5 with QEMU TCG,
768 MiB RAM and a separate writable image copied from the pristine official
image. It creates only `md-subscriber-lab`, `md-lab-br`, `md-lab-host`,
`md-lab-tap` and the namespace client. Existing interfaces, default routes,
operator `/data`, volumes and router credentials are not used.

Management TCP 18728 and incoming RADIUS UDP 13799 bind to loopback. The client
is `198.51.100.2`, with the synthetic HotSpot at `198.51.100.1`. The fixture's
forward filter permits authenticated traffic only to the dedicated host traffic
server on TCP 18080; other forwarded client traffic is dropped. Do not add a
physical interface to the lab bridge or expose the management forwards.

The launcher refuses existing resource names. It generates separate random
credentials in a 0700 directory with 0600 files. Before every command,
`subscriberchr` checks the exact live QEMU argv, process start ticks, writable
disk path and bridge members, then reads the pinned
CHR board, architecture and version. A physical board, reused PID, additional
device/forward or unexpected bridge member is refused. These are accidental
target safeguards; they do not defend against a root user rewriting the host.

By default the host bridge has no IPv4. The explicit `native-portal` action adds
only 198.51.100.10/24, protected by the owned nftables `inet md_subscriber_lab`
table. Input from the isolated bridge can reach only that address's TCP 9444;
other host input and routing through the host to other interfaces are dropped.
The exact firewall digest and sole address become part of every subsequent
ownership check. No physical interface joins the bridge; the existing default
route is retained. `stop` removes only this owned policy. A changed policy or
unexpected pre-existing table is refused rather than overwritten.

Stop validates ownership again, terminates only that QEMU, then removes only
the created namespace/TAP/bridge. Private images, ledgers and evidence remain.
An unexpected process or topology requires inspection; the tool never guesses
which physical configuration to remove.

## Prepare development tooling

Use a Linux development host with Node 24, Go 1.27, `ip`, `curl`, TAP/network
namespace privileges, nftables and sufficient RAM. Node and QEMU are lab tooling only;
they are not production dependencies. No paid CHR trial is needed. CHR Free's
throughput limit and software emulation prevent hardware performance claims.

Download [the official pinned image](https://download.mikrotik.com/routeros/7.24.5/chr-7.24.5.img.zip)
with normal TLS verification, check the ZIP CRC, and extract it to
`/dev/shm/mikrodash-subscriber-chr-lab/chr-7.24.5.img`. The raw image is
134217728 bytes. Record the downloaded SHA-256 privately; a locally computed
digest alone is not a vendor signature or published-checksum verification.

The current launcher expects QEMU at
`/dev/shm/mikrodash-subscriber-chr-lab/root/usr/bin/qemu-system-x86_64`,
its modules under `root/usr/lib/x86_64-linux-gnu/qemu`, data under
`root/usr/share/qemu` and SeaBIOS under `root/usr/share/seabios`.
On the tested Debian amd64 host, download the distribution's
`qemu-system-x86`, `qemu-system-common`, `qemu-system-data`, `seabios`,
`libcapstone4`, `libfdt1`, `libpmem1`, `librdmacm1`, `libibverbs1`,
`libvdeplug2`, `libslirp0`, `libndctl6` and `libdaxctl1` packages with
`apt-get download`, then extract them with `dpkg-deb -x` into that private
`root` directory. This does not install or restart a system service. Verify
QEMU's version and resolve missing libraries before launch. Other host layouts
need an explicit launcher/guard change; there is no fallback to a live router.

## Run and retain evidence

Run from the MikroDash checkout. Use a new absolute private directory for each
VM lifecycle; the state path below is a synthetic example. `start` and `stop`
need root for namespace/TAP operations. The Go command can be built normally.

```sh
node tools/subscriber-chr-lab.mjs start "$PWD/.temp/chr-acceptance"
go run ./cmd/subscriberchr -state "$PWD/.temp/chr-acceptance/runtime.json"
go run ./cmd/subscriberchr -state "$PWD/.temp/chr-acceptance/runtime.json" -action configure
go run ./cmd/subscriberchr -state "$PWD/.temp/chr-acceptance/runtime.json" -action configure-https
node tools/subscriber-chr-lab.mjs native-portal "$PWD/.temp/chr-acceptance"
go run ./cmd/subscriberchr -state "$PWD/.temp/chr-acceptance/runtime.json" -action prepare-runtime
go run ./cmd/subscriberchr -state "$PWD/.temp/chr-acceptance/runtime.json" -action probe-https \
  > "$PWD/.temp/chr-acceptance/protocol-https.jsonl"
node tools/subscriber-chr-lab.mjs stop "$PWD/.temp/chr-acceptance"
```

Inspection is the default. Configuration and probing are separate explicit
operations. Bootstrap refuses conflicting existing rows. Each probe uses a
new persistent SQLite ledger and the production PAP/accounting implementation.
It admits the actual isolated client with simultaneous time, download and
upload limits. `probe-https` uses CA and hostname validation for the real client
PAP POST. The explicit `probe` compatibility action uses HTTP confined to the
isolated namespace. Neither manual protocol probe is the complete portal SSO
journey. Additional HTTPS actions are `probe-download`, `probe-upload`,
`probe-byte-renew` and `probe-disconnect`.

`configure-https` generates ECDSA keys on the owned appliance, signs the lab
certificate and exports public certificates only. It never uploads or exports
a private key. The certificate export is checked as one current public PEM
certificate with the expected CA/signature/SANs. The pinned firmware requires
the singular `number` argument when signing; a retained earlier plural-argument
attempt failed rather than being ignored.

`prepare-runtime` creates fresh independent runtime-data, encrypted router
credentials, a sealed RADIUS secret and a native portal TLS identity. It refuses
an existing runtime ledger and starts its configuration in Shadow. It does not
launch MikroDash, enable Pilot or replace operator /data. Stop the separately
owned app before stopping its VM. Reviewed real local-limit, timeout, byte and
Disconnect probes are required before changing only this lab fixture to Pilot.

The probe records only selected packet statistics and synthetic identities,
never passwords or RADIUS secrets. Keep the raw directory private. Review a
whitelisted report before publishing any evidence. A failed probe remains a
failure; retained NAKs and compile/bootstrap failures are not erased.

## Observed behavior, 2026-10-20

Environment: QEMU 7.2.22, TCG, CHR `7.24.5 (stable)`,
`CHR QEMU Standard PC (i440FX + PIIX, 1996)`, x86_64.

| Probe | Actual result |
| --- | --- |
| Production PAP/accounting handler, real client POST | Access accepted; Start, standard 60-second Interim and Stop committed to a fresh v41 ledger |
| Initial local timeout | Active row showed 60 seconds |
| CoA without Framed-IP-Address | Signed NAK, Unsupported-Extension; removing other selectors did not fix it |
| Same CoA with API-confirmed Framed-IP-Address | ACK; at uptime 9 seconds the row showed 81 seconds left for a requested total of 90 |
| Extension enforcement | The original session was still present after 63 seconds, past its original 60-second deadline |
| Shortening to total 5 seconds at uptime 63 | Immediate Session-Timeout Stop and empty active table; ledger retained 63 consumed seconds |
| Two rate changes | The exact device's unique dynamic queue read back 128000/512000 and 64000/128000 upload/download ceilings |
| Download payload 65536 bytes | API bytes-out +66038; bytes-in +1146, including protocol overhead |
| Upload payload 16384 bytes | API bytes-in +16881; bytes-out +708 |
| RADIUS direction and durable final counters | Input 18027 upload, output 66746 download; final ledger held those values once, 63 seconds, stopped session, no pending receipt |

The discovered CoA defect is corrected: operation packets include only the
fresh API-confirmed device IPv4. Persisted retries must retain that same address;
a missing or changed address becomes pending-reconnect without releasing credit.
Readback cannot verify an old packet against a new address. This is independent
of the existing service guard and does not grant new live-write authority.

The lab confirms total-session timeout semantics on this appliance. It does not
enable capabilities on any enrolled operator NAS. The manual lists writable
CoA attributes but does not by itself prove target selector or timeout semantics:
[RouterOS RADIUS](https://manual.mikrotik.com/docs/authentication-authorization-accounting/radius/),
[HotSpot](https://manual.mikrotik.com/docs/authentication-authorization-accounting/hotspot-captive-portal/),
[RFC 5176](https://www.rfc-editor.org/rfc/rfc5176).

## Remaining acceptance

The normal-runtime browser/ticket path, five continuous worker renewals,
directional local byte caps and bounded outage behavior have the qualified lab
evidence below. Automatic WiFi landing/rejection handoff, continuous renewal at
hard calendar edges, total/Gigaword enforcement, proxy header attribution,
actual User Manager coexistence and operational performance budgets remain open.
Queue ceiling readback is not measured throughput. No latency/CPU/flash SLA, ARM
result, live L009/C admission or complete module acceptance is claimed.

## Development checkpoint

The focused production selector fix was delivered separately in
[PR #56](https://github.com/so7ob/MikroDash/pull/56) to verified develop
`16deeb1bff1301845a9616a2eea1a7548ac8ff84`. Required head and post-merge CI
passed. The broader #54 harness remains under development.

The launcher and exact ownership check were exercised on a second fresh VM;
start, guarded configuration and scoped stop succeeded. Three auto-discovered
lifecycle tests pass without requiring QEMU. Go ownership/evidence tests pass;
compiling/executable mutations that bypass argv or lifecycle ownership are
caught by their regressions, with exact source restored. The full #54 sweep and
remaining acceptance above are still required before its PR delivery.

A private HTTPS prototype verified the real HotSpot login document with normal
CA and hostname checks (HTTP 200). Its reverse-proxy TLS attempts reset before
reaching the diagnostic backend; forwarded-header attribution is not verified.
The prototype also found that RouterOS logs file contents supplied through
`/file/add`. Do not upload a private key that way. That disposable VM and its
one-day key were retired, with redacted diagnostics retained. Subsequent TLS
bootstrap must generate keys on the appliance and export public certificates
only. This prototype is not part of the public bootstrap command.


## HTTPS and local byte probes

Normal CA/hostname verification returned 200 for the appliance HTTPS login page
and 401 for an unauthenticated native MikroDash portal session endpoint. The
client's attempt to reach host administration on TCP 13083 was blocked by the
owned policy. The appliance Reverse Proxy TLS handshake reset or timed out
without reaching a confirmed live diagnostic backend; its forwarded headers
remain unverified. Native TLS is used for the normal-runtime journey. This is
not a claim that Reverse Proxy fails on all appliances or the operator L009.

A strengthened HTTPS timeout probe passed with explicit total-timeout assertions,
matching final API/RADIUS counters and retained independent balances. The proof
rejects malformed/fractional durations instead of parsing them as zero. Byte
readback accepts monotonic in-flight TCP acknowledgements; changed identity,
reset counters or incorrect ceilings fail.

| Actual CHR transfer | Result and limits |
| --- | --- |
| Initial download limit 65536 bytes | Interrupted a 1 MiB attempt; Stop committed 239996 download bytes, overrun 174460 bytes, subsequent HTTPS admission refused |
| Initial upload limit 65536 bytes | Interrupted a 1 MiB attempt; Stop committed 67904 upload bytes, overrun 2368 bytes, subsequent admission refused |
| Byte CoA after about 1 MiB | Cumulative download 1054227, new ceiling 1119763; same session/counters retained; transfer stopped at 1126551, overrun 6788 bytes |
| Optional 3 GiB total CoA | ACK and exact total-limit readback, without erasing directional ceilings; multi-gigabyte enforcement was not exercised |
| Production Disconnect | Base request failed; corrected method with confirmed IPv4 verified empty active table and committed Admin-Reset Stop in one 77.169 ms sample |

The large initial download burst is retained as a material measured limit; these
samples do not establish an acceptable overshoot budget. Local total/gigaword
CoA flags remain disabled for the normal-runtime lab. A sustained roughly
18.7-second download under the 512000 bit/s queue ceiling carried 1 MiB; tiny
initial transfers were bursty. Queue readback alone is not throughput proof.

The clean real MikroDash binary at `71c63233` was started with the fixture's
existing ledger and reviewed CHR-only capability evidence. The actual browser
entered credentials once, received a device-bound ticket, POSTed it to the real
HTTPS HotSpot and returned to a connected account. Fast reached verified
exact-device queue readback. The first browser test then failed because its
cooldown assertion expected HTTP 429; this API currently returns a sanitized
400. Full browser acceptance remains pending validation of that error body and
the second speed switch. The failure is retained, not reported as a passing
journey. All browsers were closed before the first normal-worker renewal.


## Normal MikroDash runtime evidence

The clean static amd64 binary at `71c63233` (tree identical to develop
`7d6f8121`) used the normal startup, shared RouterOS connection/observer, native
TLS portal, RADIUS listeners and existing synthetic persistent ledger. Five
renewals reached `verified` on one accounting session at 201 connected seconds.
Every browser had closed before the first renewal. The monitor observed the
last 128.091 seconds, retaining the same lease/session and monotonic consumption.
This exercises the real worker rather than a virtual clock or manual CoA loop.

A subsequent actual-client HTTPS reconnect retained 227 consumed seconds and
3373 spendable seconds before new admission. The final strengthened browser run
waited for each speed request response before checking verified ledger and
exact-device queue values; Fast and Economy both passed without changing the
subscription or expiry. Dashboard return took 2.105 seconds in this one sample.
An earlier run's no-op Fast check could read a previous verified DOM state while
the new request was still pending; that weak attempt is retained separately and
is not the strengthened proof. The actual cooldown response was HTTP 400 with
`wait before changing speed again`, preserving the selected profile. Screenshots
at 375 and 1280 are private synthetic evidence. The browser used two explicit
public-key pins; separate curl probes verified both CA chains and hostnames.
Automatic HotSpot landing handoff is still unconfigured: the test deliberately
navigated from the actual HotSpot document to the native portal.

With every browser closed, bounded one-way transfers traversed the real NAS;
API and RADIUS shared the production ledger. Final Stop retained 67570 download
and 32609 upload bytes. Actual admin suspension through the protected HTTP API
caused background Disconnect and committed Stop in one 1271.552 ms sample,
without losing consumption. The first administrative request was refused with
403: the fixture had created a legacy admin role string but omitted its RBAC
grant. Preparation now enrolls the sole synthetic administrator using the
existing grant API. `repair-runtime-admin` repairs only the owned fixture;
other user sets are refused. A regression exercises the data-preparation call
site and proves that repeating preparation cannot replace the existing ledger.

During 65 resource samples over 128.091 seconds, application CPU median/max was
0.5%/1.0%, RSS 39752–76184 KiB; QEMU CPU median/max was 10.495%/14.495%, RSS
410688 KiB. Application `write_bytes` increased 7229440 bytes. These process
measurements cover one idle client with renewals, not a load test, router CPU,
flash lifetime, API channel count, fsync latency or an accepted operational
budget. The write volume needs explicit assessment before router-container use.

Important verification correction: Go 1.27's `testing.T.TempDir` follows
GOTMPDIR. Earlier commands that moved GOTMPDIR to RAM also moved SQLite test
files there, contrary to the earlier local verification description. The real
runtime ledger and GitHub CI used separate ordinary storage. Subsequent local
persistence checks compile a test executable in RAM and run it with GOTMPDIR set
to an ordinary disk directory. RAM-backed checks remain behavioral checks only.


## Normal-runtime exhaustion and ordered RADIUS selection

An explicit immutable v2 synthetic subscription retained the previous account
history and supplied a new 3600-second/65536-byte grant for the exhaustion test.
This was a reasoned fixture reassignment, not a speed change or hidden reset.
The real portal/ticket/HotSpot journey connected within 2.115 seconds, after
which all browsers closed. `probe-runtime-exhaust` interrupted a 1 MiB attempt:
87708 client payload bytes, 88124 accounted download bytes, 22588 bytes over the
local limit. API absence and a committed final Stop were verified. Lifetime
retained the prior 67570 bytes plus this subscription's 88124, exactly 155694;
API/RADIUS overlap did not debit the traffic twice. Download remaining was zero.

An actual browser re-entry was refused by the real HotSpot/RADIUS path. Portal
sign-in/account access remained available and its administration route returned
404. Recovery used explicit portal navigation; automatic landing and rejection
handoff remain gaps, and are not claimed by this acceptance sample.

`probe-radius-selection` temporarily appends a synthetic secondary RADIUS
endpoint on the owned CHR only. The secondary uses the existing Go PAP/accounting
implementation, a distinct account and a separate durable test ledger with a
10-second local authorization. It is a **legacy authority stand-in, not User
Manager**. It never authenticates the primary fixture's account.

Actual CHR results: primary Access-Reject left zero active sessions and sent
zero requests to the secondary. Disabling only the owned primary allowed the
secondary's known account to connect with the observed 10-second timeout; its
Stop committed durably. Restoring the primary again rejected the legacy login
without another secondary request. A second RADIUS entry therefore cannot be
assumed to route unknown pilot users to a previous authority on this firmware.

`probe-radius-selection-fault` intentionally fails after disabling the primary.
It exits 1 with the injected failure; rollback then reads the exact configuration
back and records `verified: true`, primary enabled and secondary removed. This
is a successful rollback observation for a deliberately failed probe, not a
successful admission run. The normal selection run also restores the primary
and removes only its created secondary entry. No operator RADIUS changed.

The official [HotSpot manual](https://manual.mikrotik.com/docs/authentication-authorization-accounting/hotspot-captive-portal/)
documents `radius-default-domain` together with `/radius domain` for per-profile
server selection. The same-profile selection measurement below qualifies only the
owned lab routing behavior. A live C transition still needs an implemented domain
handoff, conflict checks against actual legacy usernames, one authority per login,
accounting/CoA acceptance and specifically approved routing/rollback.


## Real application restart and outage findings

This section retains the initial pre-#59 failed acceptance finding. The missing
finality guard was subsequently corrected; the fresh #64 acceptance below proves
current refusal after an actual outage without erasing that earlier failure.

A three-second deliberate app pause followed by restart recovered the same
actual lease/accounting session without browser, reauthentication, credit reset
or router-session replacement. The pause/restart/observation sample took
5844.684 ms; this is not a reconnect latency or five-second display SLA claim.
The public runtime controller also exercised graceful stop, preserved the
ledger and verified all four new-process listeners on restart.

A longer deliberate outage proved local enforcement: the observed remaining
authorization was 44 seconds and the complete API read saw no active client at
44615.115 ms, including polling/read latency. Accounting Stop did not recover
after the additional downtime. The normal app closed the absent session seat
and retained unresolved holds, but its snapshot still allowed a new admission.
The acceptance script failed with `unknown missing-Stop exposure still permits
new admission`. This is a production policy gap, not a passing outage acceptance
or a reason to enable a live C pilot. The focused correction must deny admission
and renewal until covering final accounting or reasoned review. The saved actual
ledger remains intact for regression/recovery testing.


## Normal runtime and browser replay

The five protocol probes below and the normal runtime bind the same primary
lab RADIUS ports. Stop the owned runtime before those probes. Never start a
second accounting writer against its ledger. The ordered selection probes are
different: their secondary binds separate ports and the normal primary must
remain running, as described below. On a fresh configured VM, run each HTTPS
probe separately and retain its exit status and private JSONL output:

```sh
umask 077
set -euC
lab_dir="$PWD/.temp/chr-acceptance"
for probe in https download upload byte-renew disconnect; do
  go run ./cmd/subscriberchr -state "$lab_dir/runtime.json" -action "probe-$probe" \
    > "$lab_dir/protocol-$probe.jsonl"
done
```

Do not continue after a failed command. Review actual timeout extension and
shortening, queue ceilings, one-way counters, local cap overrun and final Stop.
An ACK alone does not meet this review. The current demonstrated semantics are
`timeoutMode: total`, `byteMode: total`, `counterMode: input-upload`, and
`coaRate: true`. Keep `coaTotal` and `coaGigawords` false: readback is not proof
of multi-gigabyte enforcement. This review applies only to that owned VM,
version and lifecycle, never to an enrolled operator router.

For the explicit lab-only Pilot review, retain a 0600
`pilot-capability-review.json` in the 0700 lab directory with these fields:

- `vm`, `startTicks`: the exact current `runtime.json` identity.
- `version`: the complete inspected RouterOS version string.
- `review`: the reasoned reviewer statement and any unverified capabilities.
- `source`: the tested commit SHA.
- `sha256`: a nonempty map from each reviewed JSONL filename without its
  `.jsonl` suffix to its SHA-256 hex digest. Names accept lowercase letters,
  digits and hyphens. All five probe outputs above must be retained.

While the runtime is stopped, change only this synthetic `runtime-config.json`:
set mode to `pilot`, `allowCoA`, `allowDisconnect`, `localLimitsVerified` to true,
and its sole client's `capabilities` to the reviewed version, absolute review
file path and the semantics above. Preserve its sealed secret, Pilot account,
network and listener addresses. `prepare-runtime` already created these in
Shadow. The runtime tool checks VM ownership, the sole encrypted loopback NAS,
private file modes and the hashed evidence; it does not replace semantic review
or authorize a live cutover.

Build the current application and frontend, then use the process controller:

```sh
go run ./cmd/webbuild -dir web
go build -o "$lab_dir/mikrodash" ./cmd/mikrodash
node tools/subscriber-chr-runtime.mjs start "$lab_dir" "$lab_dir/mikrodash"
node tools/subscriber-chr-runtime.mjs check "$lab_dir"
```

The normal binary uses the independent ledger, native portal TLS, shared router
connection and production background worker. Process ownership receipts pin PID,
start ticks, argv, binary and configuration hashes. Four listener inodes must
belong to that process. Restart with `stop` followed by `start`; do not replace a
running binary or guess which development process to kill.

Using the existing development browser harness, set
`SUBSCRIBER_BROWSER_MODULE` to its directory containing Playwright and
`@axe-core/playwright`; optionally set `SUBSCRIBER_CHROMIUM` to its installed
Chromium executable. No browser or Node service becomes a production dependency.
The command launches the browser inside the isolated client namespace, validates
both TLS chains/hostnames with curl and pins just those two certificate keys in
Chromium. It never disables all TLS verification.

```sh
node tools/subscriber-chr-browser.mjs journey "$lab_dir"
go run ./cmd/subscriberchr -state "$lab_dir/runtime.json" -action runtime-status
```

`journey` needs an eligible account with no active session. It performs the
browser HTTPS POST, verifies the actual session, tests cooldown and two distinct
speed profiles against persisted/read-back ceilings, then closes every browser.
A result is complete only with `complete.json`; earlier network-pass output does
not prove subsequent responsive/accessibility checks passed. Images and raw
account/packet evidence stay private.

`probe-runtime-traffic` measures directional transfers on the existing real
runtime session. `probe-runtime-exhaust` requires a deliberate synthetic plan
version with a 65536-byte download limit and 3600 seconds; it consumes that test
allowance. It does not reassign plans or erase history. After exhaustion,
`subscriber-chr-browser.mjs exhausted` verifies genuine re-entry refusal and
continued account access. Do not run this case on the default 16 MiB allowance
and interpret failure to exhaust as success.

For a retained missing-Stop outage ledger with one pending final reading and
positive known credit, use `subscriber-chr-browser.mjs reconciling`. It requires
`reconciling`, performs a real refused HotSpot POST, and checks unchanged
confirmed time/bytes, no active session, safe portal access and all six
responsive/theme accessibility variants. The saved CHR outage ledger passed
this case on clean application commit `1abd51de` after the #59 correction.
A covering final Stop or reasoned review remains required; the test does not
invent final counters to release credit.

`logout-runtime` verifies the owned current device over HTTPS before logout.
The ordered selection probes are `probe-radius-selection` and
`probe-radius-selection-fault`; keep the normal primary runtime running and
start with an empty owned HotSpot. They bind only the secondary test ports.
The latter deliberately exits 1 and must show verified rollback, not be counted
as a green admission test. Its secondary is a synthetic legacy service, not UM.

Before retiring the VM:

```sh
node tools/subscriber-chr-runtime.mjs stop "$lab_dir"
node tools/subscriber-chr-lab.mjs stop "$lab_dir"
```

This preserves the database, keys and private evidence. VM stop refuses a live
recorded application process. The owner VS Code preview, physical interfaces,
operator data, router A/B/C and production authentication are separate.


## Fresh final-source replay

A new VM/disk and new runtime-data were prepared with the public launcher and
clean lab CLI at `a60409cb` (the subsequent `6387693d` change corrects only guide
lifecycle wording). Fresh preparation granted the sole synthetic administrator;
the protected administration API returned 200 in Shadow without a manual grant
repair. The owned runtime was then stopped before the five primary-port probes.

All five HTTPS probes completed successfully on new durable ledgers: timeout
extension/shortening and directional counters, initial download cap, initial
upload cap, cumulative byte CoA and production Disconnect with final Stop.
This replay measured 171724 download bytes against a 65536-byte cap (106188
bytes over), and 174724 upload bytes against that cap (109188 bytes over).
Cumulative download renewal overran by 6212 bytes. Together with the earlier
samples these show material variation; none defines a guaranteed operational
upper bound. In particular, the earlier 2368-byte upload sample is not a safe
budget for future sessions. Total-attribute readback still does not prove
multi-gigabyte/Gigaword enforcement; both capability flags remain false.

Only the stopped synthetic fixture was changed to Pilot after reviewing and
hashing all five successful probe outputs for that exact VM/version/lifecycle.
The clean normal application at `1abd51de` passed the public `journey`: actual
HTTPS device-bound login, account return in 2113 ms, cooldown, two distinct
verified speed profiles and all six responsive/theme axe checks. The same
session then reached 238 connected seconds and five verified worker renewals
with every browser closed. Owned HTTPS logout followed. Original outage ledgers,
images and evidence were retained; no operator router or data was changed.

### Reviewed landing installation

After building the frontend and preparing the isolated runtime, install only the
owned fixture's reviewed handoff directory while no synthetic session is active:

```sh
go run ./cmd/subscriberchr -state "$lab_dir/runtime.json" -action install-landing -web web/dist
# Restore the original override while idle; retain the new files and ledger.
go run ./cmd/subscriberchr -state "$lab_dir/runtime.json" -action rollback-landing
```

Both actions refuse another VM lifecycle, a changed review/profile, a busy
HotSpot, unknown files or differing readback. Do not copy these commands onto an
operator router. `journey`, `exhausted` and `reconciling` browser actions now require
the rendered landing: they navigate to the NAS once, submit one portal credential
form, perform the actual browser HTTPS HotSpot POST, and assert the owned account
return. Browser storage is blocked, so the owned return URL must carry context.
All three actions passed against the actual rendered landing in the fresh #64
acceptance below. An invocation without its own `complete.json` remains incomplete.

For a separate build directory, set `SUBSCRIBER_CHR_WEB` to its absolute path
when starting the owned runtime. It must contain built `subscriber.html` and
`subscriber.js`. This avoids replacing assets served by an existing development
runtime. Its resolved argument is retained in the process ownership receipt.

### Fresh automatic return and disruptive acceptance (#64)

After the owner finished the interactive CHR trial and approved the disruptive
synthetic cases, the owned viewer stopped and audited immutable plan assignments
prepared separate exhaustion and outage subscriptions. Previous subscriptions,
lifetime totals and journal records were retained. No production NAS was touched.

| Actual normal-runtime case | Observed result |
| --- | --- |
| Automatic landing and single credential form | One portal login, one connect request, one genuine NAS HTTPS POST; owned confirmed return; both speed readbacks pass |
| 65536-byte download cap with simultaneous time/upload limits | 1 MiB attempt interrupted; 89148 client payload bytes and 89564 accounted download bytes; complete API absence and final Stop; 24028 accounted bytes over cap |
| Exhausted re-entry | Genuine RADIUS refusal and automatic owned return explaining exhausted download; account available; administration 404; confirmed balances unchanged |
| 91.679-second application outage | Last local remaining timeout 26 seconds; complete API absence at 25874.270 ms after stop, including polling/read delay |
| Missing Stop after same-ledger restart | Seat closes; one pending final reading, positive known credit and `reconciling`; genuine RADIUS re-entry refusal and automatic account recovery; no free credit |

Each browser action completed all six 375/1280 and blue/green/purple axe/overflow
checks with session storage blocked. Two fresh success return samples were
2166 and 2183 ms. The shared worker had already retained one earlier session for
19 verified renewals with every browser closed. These are individual virtual
router measurements, not target SLA or an approved operational overrun budget.
The incomplete transfer took 12011 ms including its client timeout; this is not
a disconnect-latency measurement.

Fast rejection exposed CDP response-body lifetime races in the test harness.
Failed attempts remain private; the passing harness captures clones in the
requesting browser before returning the real API response to the shipped page.
It leaves all requests, responses and NAS form traffic unchanged. Private full
evidence remains in `.temp/subscriber-chr-entry-64`, including the unresolved
outage ledger. A covering final reading or evidence-backed review is still
required to restore that synthetic account's network eligibility. Never restore
an old database or invent a final counter to make this case green.


## Same-profile domain selection measurement

Issue #70 adds `probe-radius-domain` and `probe-radius-domain-fault`. Keep the
normal primary runtime running, require an idle owned HTTPS HotSpot, and run:

```sh
go run ./cmd/subscriberchr -state "$lab_dir/runtime.json" -action probe-radius-domain
# Expected exit 1 for this deliberately injected failure; inspect rollback too.
go run ./cmd/subscriberchr -state "$lab_dir/runtime.json" -action probe-radius-domain-fault
```

The probe adds one synthetic `md-pilot` domain endpoint. It measures both list
orders and both `split-user-domain` settings with three HTTPS client usernames.
The endpoint uses the normal authenticated Shadow RADIUS handler and signs
Access-Reject; it never grants a session. NAS counter deltas verify requests,
rejections and absence of retransmissions, timeouts, bad replies or admissions.
A positive control temporarily selects the endpoint with the documented profile
`radius-default-domain`, proving that zero-request results are meaningful.

Actual isolated CHR 7.24.5 results on 2026-10-21:

| Domain endpoint position | Split user/domain | Submitted username | Selected endpoint |
| --- | --- | --- | --- |
| Last | No or Yes | Plain or domain-qualified | Existing primary |
| First | No | Plain or domain-qualified | Existing primary |
| First | Yes | `legacy-lab` | Existing primary |
| First | Yes | `md-pilot\legacy-lab` | Domain endpoint only |
| First | Yes | `legacy-lab@md-pilot` | Domain endpoint only |

Both qualified forms reached the secondary as username `legacy-lab`, with domain
and realm `md-pilot`. Plain logins never reached it. All 12 matrix attempts and
the positive control were rejected, with zero active sessions and no transport
counter errors. This is an endpoint selection measurement, not an Internet
admission, accounting, CoA, User Manager or L009/C acceptance test.

Cleanup is registered before the first potentially ambiguous write. Normal
completion restored the original profile fields and exact ordered non-secret
RADIUS configuration. The fault command deliberately exited 1 after the profile
change, then reported `domain-rollback` with `verified: true`. Unit call-site
cases also cover lost add/set replies, evidence-write failure, moved-first entry
cleanup and refusal to overwrite changed or ambiguous configuration. A process
kill can prevent deferred cleanup: retain the private baseline and inspect the
owned appliance before another probe; never copy a lab rollback to a live router.

This result does not enable the production portal's domain handoff. Before a C
pilot, verify existing names containing `@` or `\`, because enabling splitting
can change their interpretation. Preserve account names and Unicode data. Check
the default domain, all matching RADIUS services/order, local-user precedence,
UDP reachability and accounting authority. Then test actual session identity,
renewal, speed readback, exhaustion and rollback under a scoped approval.
