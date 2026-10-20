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
disk path, bridge members and lack of a host IPv4 address, then reads the pinned
CHR board, architecture and version. A physical board, reused PID, additional
device/forward or unexpected bridge member is refused. These are accidental
target safeguards; they do not defend against a root user rewriting the host.

Stop validates ownership again, terminates only that QEMU, then removes only
the created namespace/TAP/bridge. Private images, ledgers and evidence remain.
An unexpected process or topology requires inspection; the tool never guesses
which physical configuration to remove.

## Prepare development tooling

Use a Linux development host with Node 24, Go 1.27, `ip`, `curl`, TAP/network
namespace privileges and sufficient RAM. Node and QEMU are lab tooling only;
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
go run ./cmd/subscriberchr -state "$PWD/.temp/chr-acceptance/runtime.json" -configure
go run ./cmd/subscriberchr -state "$PWD/.temp/chr-acceptance/runtime.json" -probe \
  > "$PWD/.temp/chr-acceptance/protocol.jsonl"
node tools/subscriber-chr-lab.mjs stop "$PWD/.temp/chr-acceptance"
```

Inspection is the default. Configuration and probing are separate explicit
operations. Bootstrap refuses conflicting existing rows. Each probe uses a
new persistent SQLite ledger and the production PAP/accounting implementation.
It admits the actual isolated client with simultaneous time, download and
upload limits. Its initial HTTP-PAP leg is confined to the isolated namespace;
it is **not** the required HTTPS single-sign-in portal acceptance path.

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

The real CHR HTTPS portal/browser ticket path, normal shared observer and five
continuous production-worker renewals remain to be exercised. Local byte-cap
enforcement/renewal, total/Gigaword CoA support, Disconnect selectors, proxy
header attribution, User Manager coexistence, outages/restarts and performance
budgets also remain open. Queue ceiling readback is not measured throughput.
No latency/CPU/flash SLA, ARM result, live C admission or complete module
acceptance is claimed by these probes.

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
