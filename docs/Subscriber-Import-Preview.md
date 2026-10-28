# User Manager source review

The existing `subscribercheck -user-manager` path remains read-only. Its preview
reads the same five User Manager definition menus and requests only the existing
non-credential properties. It has no apply operation, creates no accounts or plans,
and writes no opening balances. Source reads happen sequentially and are not an
atomic snapshot. No live router acceptance is established by a report.

The preview returns the existing `rows`, `warnings` and `fingerprint` fields,
plus structured review information:

- `unavailable`: menus whose reads failed. They are absent from `rows`; a successful
  empty read appears as an empty array. Driver errors are not copied into output.
- `problems`: source issues identified by `menu`, zero-based `row`, `field` and
  `code`. `missing-identity` and `duplicate-identity` identify empty/repeated `.id`
  values and entity names. Every duplicate occurrence is flagged. Names are
  compared exactly, without case folding, trimming or Unicode normalization.
  `missing-reference` identifies an absent profile/user/limitation link field.
- `fingerprintVersion`: currently 2. The fingerprint hashes structured JSON
  containing that version, projected menu rows and unavailable-menu names. It
  includes menu boundaries, preserves row order, distinguishes missing properties
  from explicit zeros and distinguishes empty reads from unavailable reads.

Returned rows are projected onto the same requested property list and copied;
unexpected fields such as credentials are not retained, even if a reader returns
them despite the proplist. Existing name-versus-uptime/reset warning heuristics
remain warnings; names do not override source values. Raw duration, rate, zero
and reset strings are not converted into MikroDash policy semantics.

Version 2 is deliberately incompatible with the earlier unversioned fingerprint
of concatenated textual row maps. The old representation could collide across
menu boundaries or values containing map-like delimiters. Do not compare old and
new fingerprints as if their algorithms were identical. This fingerprint is a
change-detection aid, not a signed authorization token or import idempotency key.

No problem-free report means import approval. Reference target resolution,
source-to-plan mapping, credential handover, approved current-period opening
balances, transactional idempotent apply, cutover and rollback remain incomplete
under #38. Historical totals still cannot become current-period balances without
an explicit reviewed policy. Actual L009/C acceptance under #76 remains untested.

## Verification scope

Synthetic-reader tests assert read-only proplist commands, no unrequested-field
retention, source-copy isolation, missing versus zero preservation, unknown versus
empty menus, all duplicate occurrences, missing references and structured
fingerprint collision regressions. A fuzz property checks that arbitrary extra
fields cannot alter the complete report. These exercise the actual preview code
without a router or database and do not substitute for hardware acceptance.

Local Go 1.27.3 evidence for this increment: all focused preview tests passed,
174361 fuzz cases passed in ten seconds, and `cmd/subscribercheck` built. Three
compiling mutants (unfiltered rows, suppressed duplicate diagnostics and the old
flattened fingerprint) were killed by behavioral assertions, then source bytes
were restored. Full package/SQLite and real-router acceptance are separate gates;
the local pinned SQLite archive remains unavailable under the environment policy.
