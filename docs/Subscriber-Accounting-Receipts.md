# Durable accounting receipts

Migration 41 adds `subscriber_accounting_receipts` to the existing subscriber ledger.
It does not create another accounting authority. Only authenticated NAS accounting
enters this path: the mature RADIUS packet server verifies its keyed authenticator
and source secret before the handler. Unknown sources do not reach storage.

## Presence and exact values

A receipt retains the owned Class/lease, NAS, network, device, session, status,
first receipt time and source time. Only five whitelisted statistical attributes
are retained in `fields`. Passwords, tickets, authenticators, arbitrary attributes
and whole packets are excluded. Malformed or repeated integer attributes retain
hex evidence instead of accepting their first value. Distinct malformed bytes
remain distinct evidence.

Raw unsigned wire counters use exact decimal JSON strings, including Gigawords.
`timeKnown`, `downloadKnown` and `uploadKnown` describe complete valid wire fields;
they do not promise that a uint64 fits the signed ledger range. Missing, malformed,
unsupported, known zero and unlimited policy states remain distinct. RADIUS input
is subscriber upload; output is download. API direction still requires separate
version-bound target evidence.

Only a wholly omitted initial Start statistic defaults to zero. A high word
without a low word, a malformed Start field or duplicate integers is incomplete.
Interim and Stop require complete time and directional counters before applying
that reading. Valid fragments are retained as floors for later settlement.

## Commit and acknowledgement

Financial receipt identity includes normalized ownership, status, counter presence,
values and original statistical evidence. It excludes packet identifier,
authenticator, Acct-Delay-Time and arrival time. A retry retains the first source
and receipt timestamps, so a changing transport envelope cannot debit again.

An immediate transaction owns these outcomes:

- `applied`: the receipt and cumulative high-water debit commit atomically.
- `pending`: authenticated owned evidence is durable, but incomplete statistics
  cannot become an inferred zero, debit or released delegation. Only after that
  commit is an Accounting-Response sent.
- `covered`: a reliable cumulative reading settles the retained evidence. The
  original payload remains; state/resolution columns record the transition.
- `rejected`: ownership or supported status does not match. Evidence is retained
  without account ownership; no success response is sent.

If a complete reading fails to debit, the transaction rolls back. The original
receipt is retained in a separate transaction if storage is still available, but
no response is sent. A retry must still apply it successfully. Receipt insert,
audit, coverage or commit failures likewise cannot produce an early success.
SQLite startup continues to require synchronous FULL. Ordinary filesystem fault
and close/reopen tests do not prove recovery from storage hardware power loss.

Accounting-On/Off markers are router-scoped audited evidence. They do not reset
balances, guess the first HotSpot network or close every lease. Their presence
alone is not a trustworthy final counter snapshot.

## Recovery and admission

Pending evidence survives a real SQLite close/reopen. Snapshots retain confirmed
values, expose `pendingAccounting`, report `reconciling` and explain the admission
block. New reservations and renewal/rate authorizations are refused while the
account has unresolved receipts. Existing sessions retain their already-issued
local limits; pending telemetry never removes those outage bounds.

A verified same-session API interim may cover an older incomplete interim when
its time and all recorded floors are covered. It cannot settle a final Stop or
clear a newer receipt. API ingress is explicitly interim-only. A final RADIUS
Stop may settle incomplete Stop evidence; it must cover recorded byte floors.
One second of time-only rounding retains the largest trustworthy second instead
of changing the account to Unknown. Bytes receive no such tolerance.

A late partial packet already covered by a committed complete Stop is stored as
covered, without reopening the account or recharging usage. Contradictory known
counters remain pending. A reasoned final-reading correction requires a trusted
absence settlement or a committed final Stop and cannot modify an active session.
It may add newly established usage, never subtract existing cumulative usage.
Duplicate approval has no second debit. Old lease identity cannot close a later
connection even if RouterOS reuses its Acct-Session-Id.

The existing reconciliation API returns account-scoped receipts alongside gaps,
holds and final-reading reviews. Its administrator RBAC, Origin and audit checks
remain in place. The portal receives only its owned snapshot and pending count;
raw administrative receipts are not exposed through a new public endpoint.

## Evidence and remaining acceptance

Real UDP tests cover incomplete Stop retries, storage failure/no response,
unknown NAS, authenticator rejection and later complete recovery. Parser tests
cover missing/zero/malformed/duplicate fields, direction, Gigawords and values
above 2^53. Recovery tests cover overlapping API/RADIUS readings, late partial
Stop, one-second preservation and SQLite close/reopen. A fuzz property checks
range decisions and retry identities. Chromium exercises the existing protected
reconciliation workflow against real isolated SQLite and synthetic NAS data.
Built native-TLS and trusted-proxy portal journeys also assert the pending
notice and retained confirmed time/remaining cards against the actual owned API.

See [verification](Subscriber-Verification.md) for exact commands and outcomes.
Full #38 acceptance remains open: actual C browser admission, measured direction,
local-limit/CoA semantics, reboot/outage behavior and operational timings still
require an explicitly scoped target pilot.
