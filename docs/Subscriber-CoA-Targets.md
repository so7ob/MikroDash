# Confirmed HotSpot dynamic authorization targets

The rate and renewal worker uses the current shared API observation to select
the owned device's IPv4 address. It includes that address as RADIUS
`Framed-IP-Address`, alongside the username, device, HotSpot server and accounting
session identity already checked by the worker. A browser does not supply this
address. Existing service grants, capability evidence and policy checks remain
required; this correction does not enable a disabled or Shadow writer.

Missing, malformed, loopback, multicast, unspecified and IPv6 values cannot
create a CoA packet. A persisted packet must contain exactly one matching IPv4.
Its missing or changed address becomes `pending-reconnect` before either retry
or readback verification. Matching queue rates at a different address cannot
verify an older intent. The refusal preserves reservations, usage, subscription
identity and expiry. Existing packets are not rewritten into a new target.

## Real CHR evidence

An isolated, disposable x86 CHR 7.24.5 running under QEMU 7.2.22 TCG on
2026-10-20 admitted an actual network-namespace HotSpot client through the
production Go PAP/RADIUS handler and a separate persistent SQLite v41 ledger.
No operator router, data store, credentials or physical network was used.

The original selectors returned a signed `CoA-NAK` with
`Unsupported-Extension`. Removing Called-Station-Id and reducing the other
selectors did not fix it. Adding the device's API-confirmed Framed-IP-Address
to the original request returned ACK and changed the observed session timeout.

For a 90-second request, uptime 9 seconds plus 81 seconds left demonstrated
total-session semantics. The same session remained after its original
60-second admission timeout. Shortening the total to 5 seconds at uptime 63
caused immediate Session-Timeout Stop and an empty active table. The ledger
retained 63 seconds consumed with no pending accounting receipt.

Two rate requests read back as 128000/512000 and 64000/128000 upload/download
ceilings on the unique dynamic queue for the exact client. These are configured
ceilings, not measured throughput. One-way payloads confirmed API input as
upload and output as download. RADIUS Interim/Stop and the final ledger agreed:
18027 upload bytes, 66746 download bytes, counted once.

These are protocol-lab results, not L009 acceptance, a five-cycle normal-worker
renewal run, an HTTPS single-sign-in journey or performance measurements.
The broader lab workflow continues in
[#54](https://github.com/so7ob/MikroDash/issues/54); the complete module remains
open in [#38](https://github.com/so7ob/MikroDash/issues/38). No deployed NAS
capability flags or operator router settings were changed.

## Disconnect selectors, Issue #57

The production Disconnect path also includes the fresh API-confirmed IPv4,
alongside ledger-resolved username, device and accounting session ID. It checks
the shared observation again after the existing service/session guard. Absent,
stale, future, invalid-address or different-device/network/start observations
are refused before intent audit and network I/O. Browser values cannot provide
the address. An acknowledged request retains its lease and reservations until
final accounting or evidence-backed reconciliation.

On the same isolated CHR 7.24.5, the original production Disconnect was rejected.
A diagnostic request adding the confirmed address returned Disconnect-ACK and
an Admin-Reset Stop. After the focused production correction, the actual
Disconnect method returned ACK; an empty active table and a committed Stop
verified removal in one 77.169 ms sample. This is a single unloaded lab sample,
not a latency bound or L009 acceptance. No fallback packet was used in the
successful production run.

Real UDP regressions assert exact selectors, fail-closed observations, ledger
ownership, service authority, audit failure and unchanged credit on ACK/NAK.
The watcher-free background test now uses a shared observation rather than a
permissive callback alone.

The [RouterOS RADIUS manual](https://manual.mikrotik.com/docs/authentication-authorization-accounting/radius/)
lists writable CoA attributes, while [RFC 5176](https://www.rfc-editor.org/rfc/rfc5176)
defines session identification and response behavior. Documentation alone does
not prove this firmware's selector requirements or timeout interpretation.

## Regression coverage

- The five-renewal UDP NAS now requires the exact Framed-IP-Address.
- Packet tests reject invalid addresses, permit an exact persisted retry and
  reject absent or changed selectors.
- A processor regression presents a matching rate on a changed API address.
  The issued intent must remain unverified, become pending-reconnect and retain
  all credit, identity and validity.

The [verification record](Subscriber-Verification.md) records commands and actual
results. No live deployment or production cutover follows from this correction.
