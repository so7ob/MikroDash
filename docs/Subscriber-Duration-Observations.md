# Trusted HotSpot duration observations

The subscriber observer validates complete RouterOS duration fields before they
can identify a session or verify a bounded local timeout. This parser is specific
to subscriber accounting; the legacy `routeros.DurationSeconds` helper and its
collector behavior remain unchanged. No schema, credential or quota migration
is required and no new RouterOS command/channel is introduced.

A missing, malformed or unsupported active uptime makes that observation
incomplete. It cannot become a successful empty active read, close a concurrency
seat, synthesize final accounting or release a reservation. The last trusted
ledger observation and cumulative usage remain intact. Missing or malformed
`session-time-left` is unknown while valid presence and traffic counters remain
usable. Explicit zero is known zero and retains its separate meaning.

Accepted shapes are nonnegative decimal seconds, ordered week/day/hour/minute/
second/millisecond units, and a clock with optional week/day prefix. Clock
minutes and seconds are 0..59; fractional clock seconds have at most nine
digits. Validated milliseconds and clock fractions are floored to integer
seconds. Repeated/out-of-order units, partial matches, signs, whitespace,
unsupported precision and overflow are refused. Checked uint64 components and
sums are bounded to 315360000 seconds, matching the ledger's supported session
range; architecture-sized int and nanosecond multiplication are not used.
This is a field-specific duration bound, not a traffic/rate quantity bound.

The [HotSpot manual](https://manual.mikrotik.com/docs/authentication-authorization-accounting/hotspot-captive-portal/)
describes active uptime and remaining timeout as time values. The official
[scripting reference](https://help.mikrotik.com/docs/spaces/ROS/pages/47579229/Scripting)
shows week/day/clock forms and fractional seconds. These documentation examples
are not claims that every HotSpot firmware emits every supported shape. Actual
CHR 7.24.5 short-session h/m/s observations are separate qualified evidence;
long-duration, malformed and overflow cases are automated fixtures.

## Verification record

Regressions first fail on develop `3bfdbd25` at the real shared observer call
site: garbage/partial/overflowing uptime becomes a complete snapshot, nonempty
invalid timeout becomes known zero, and a corrupted uptime closes a previously
observed active lease as absent. The corrected observer preserves the exact
prior snapshot and leaves invalid timeout unknown without erasing valid bytes.
Explicit zero remains known. Supported spelling and boundary tests accompany a
fuzz property checked against independent arbitrary-precision arithmetic.

The focused race executable passes with test GOTMPDIR/TMPDIR on ext4 `/tmp`.
The first race compilation failed for lack of space in the task-created RAM
build filesystem; it ran no race tests. Only completed task binaries and old
compiled data entries from that task's RAM cache were reclaimed, with a retained
manifest. Data, images, operator files and volumes were preserved. Successful
source gates, mutation/randomized tests, actual CHR and exact-head/post-merge CI
must be recorded separately; this document does not imply L009 acceptance.
