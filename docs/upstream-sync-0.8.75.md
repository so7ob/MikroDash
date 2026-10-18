# Upstream synchronization through 0.8.75

Issue #31 integrates SecOps-7/MikroDash through
`edff3060446582d75159cedbe2560a4466c38b84`. The authorized merge commit preserves
upstream ancestry while retaining the so7ob release process and Activity Analytics.
The synchronization keeps AppVersion at 1.0.0; the separate release preparation
sets 1.0.1 once.

The combined schema is version 37: Activity remains migration 35, DNS lists use
36, and device watch/Wi-Fi history use 37. The fresh DDL includes all three.
`TestUpstreamMigrationsPreserveForkData` upgrades synthetic version-34 and
version-35 stores, compares their tables and indexes with a fresh database,
checks Activity data, grants and settings, and repeats migration safely.

DNS jobs retain their initiating user ID and originating feature, with an explicit
system actor for scheduled lists. Current policy is checked inside each queued
write and while waiting for its allowance. Stopped jobs retain their reason;
unconfirmed removals stay pending. A definition saved during cleanup survives
reconciliation and completion of the old request.

Regression evidence is in `internal/server/dnsjob_test.go` and
`internal/session/needs_test.go`. Disabling job policy checks, accepting unconfirmed
removals, discarding a newer saved definition, or dropping Activity's session hold
must each fail its corresponding regression test. Mutation runs use Go overlays
and do not edit the source tree. Exact revision and local/CI results are recorded
in the delivery PR.

The ten optional features remain off by default. Browser acceptance uses the real
modules and markup with synthetic transport; see `web/test/README.md`. The existing
Activity page, dashboard and library browser journeys remain separate acceptance
commands. No physical-router conformance or production-data migration is claimed.

## RouterOS references

The imported DNS vocabulary and CAP upgrade paths were checked against the
[official DNS manual](https://manual.mikrotik.com/docs/network-management/dns/),
[DNS static CLI](https://manual.mikrotik.com/docs/cli-reference/ip/dns/static/),
[DNS forwarders CLI](https://manual.mikrotik.com/docs/cli-reference/ip/dns/forwarders/),
[DNS adlist CLI](https://manual.mikrotik.com/docs/cli-reference/ip/dns/adlist/),
[modern CAP upgrade CLI](https://manual.mikrotik.com/docs/cli-reference/interface/wifi/capsman/remote-cap/upgrade/)
and [legacy CAP upgrade CLI](https://manual.mikrotik.com/docs/cli-reference/caps-man/remote-cap/upgrade/).
The nine static record types include FWD, MX, NS and SRV. A DNS forwarder is a
separate resource; a FWD entry can reference it. Command documentation establishes
supported vocabulary, while the synthetic regressions establish application
behavior. These checks do not substitute for testing particular RouterOS hardware.
