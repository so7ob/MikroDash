# Subscriber accounting reconciliation

Subscriber Management keeps confirmed usage separate from delegated reservations and
provisional exposure. A counter snapshot crossing a calendar boundary cannot prove
how its bytes were distributed. Migration 40 records that delta once in the journal
and account lifetime totals, with a provisional hold in each eligible period. It
does not present the full delta as measured consumption in multiple periods.

## Authority and migration

The existing MikroDash SQLite ledger is authoritative. No second subscription or
accounting database is introduced. Migration 40 adds `subscriber_byte_gaps`,
`subscriber_gap_periods` and `subscriber_legacy_exposure`; it retains all events,
subscriptions and prior balances. Existing `boundary-conservative` period values
move into immutable legacy upper-bound records and unavailable provisional exposure.
The migration is repeatable. It releases no balance without a recorded settlement.

API `used` is confirmed or explicitly approved period usage. `reserved` includes
outstanding delegation and unresolved exposure; `provisional` identifies the latter
subset. Available balance subtracts reserved once. The interface displays these
components separately. Quantities are non-negative signed 64-bit decimal **strings**
in JSON, in bytes or integer seconds. Speed selection changes none of them.

## Administration

Open Subscriber Management → Reconciliation. The existing global
`system:subscribers` permission is required at use time. Subscriber portal listeners
do not serve these routes. Choose an account and load its records. Each approval
requires a reason, an evidence reference and explicit confirmation.

- Byte distribution: allocate exactly the original delta among eligible periods.
  Every period may appear at most once. Zero allocations are allowed; omitted
  eligible periods receive zero. This is approved distribution, not measured traffic.
- Missing Stop: after the background service has proved absence beyond the maximum
  issued delegation, approve non-regressing final cumulative seconds/download/upload.
  Approval settles only the relevant session. Last reliable values are shown for
  review; they are not proof that no additional traffic occurred. New admission and
  renewal remain paused until final accounting is settled, even if the session has
  zero commercial quota holds. Ordinary period-allocation holds are separate.
- Legacy conservative balance: approve usage within the retained original bound.
  The original value and its quality remain available for audit.

Approvals, usage changes, hold resolution and audit append commit atomically. Identical
replays are idempotent; a conflicting byte/legacy approval cannot overwrite an existing
approval. Lifetime totals are not recalculated from the sum of period allocations.
A failed audit aborts the entire transaction. Changing the selected account clears
its displayed records, and late responses cannot repopulate another account's view.

## HTTP API

`GET /api/subscribers/reconciliation?account=<id>` returns arrays `gaps`, `legacy`
and `sessions`, including immutable IDs, relevant quantities and period timestamps.
The review queue returns up to 1,000 records per category, with unresolved byte/legacy
records first. Approvals let later pending records enter the queue; this is not a
complete paginated usage-history endpoint.
An unresolved `resolvedAt` is Go's zero timestamp, `0001-01-01T00:00:00Z`.
Responses are scoped to the requested account and require administrator authorization.

`POST /api/subscribers/reconciliation` requires the admin session, same-origin
`Origin`, JSON content and `X-MikroDash-Request: subscriber-admin`. Unknown fields
are rejected. The body shares `action`, `id`, `reason` and `evidence`:

```json
{
  "action": "byte-gap",
  "id": "synthetic-gap-id",
  "reason": "Approved distribution",
  "evidence": "Synthetic approval record; distribution is not measured",
  "allocations": [
    {"periodStart": "2030-10-31T00:00:00Z", "amount": "40"},
    {"periodStart": "2030-11-01T00:00:00Z", "amount": "60"}
  ]
}
```

For `missing-stop`, supply `reading` with the same `leaseId` as `id`, and cumulative
`seconds`, `download`, `upload` decimal strings. For `legacy`, supply `amount`.
Success is `{ "ok": true }`; refresh the account view after success. Invalid input,
non-conserving allocations, unavailable evidence and conflicting approvals return a
sanitized error without changing the ledger. Authentication/origin/permission failures
use HTTP 401/403. Validation/settlement errors currently use HTTP 400.

No RouterOS command is sent by these endpoints. Background enforcement subsequently
uses the resulting policy decision through its existing service guards.

## Verification and limits

Unit/integration tests cover conservation, idempotency, retained history, 64-bit
remaining-balance saturation, anonymous refusal, audit fault rollback and competing
approvals. The explicit browser test uses real protected HTTP handlers and SQLite,
with synthetic accounts and no NAS. It checks incorrect-sum refusal, evidence
retention, approved balances, account-response races, Unicode data, keyboard tabs,
focus restoration, 375/1280 layouts and axe. Run:

```sh
go test ./internal/subscriber ./internal/db ./internal/server
go test -race ./internal/subscriber -count=1
# Configure the same external Playwright/Chromium variables as Subscriber-Verification.md.
go test -tags=subscriberbrowser ./internal/server \
  -run '^TestSubscriberAdminReconciliationBrowserAcceptance$' -count=1 -v
sh tools/verify.sh
```

The reconciliation UI does not infer unmeasured byte distribution, erase history,
perform User Manager import/cutover or establish target-router acceptance. Incomplete
RADIUS counter-payload handling, bounded relative CoA semantics, seamless hard-calendar
renewal, full administration/setup/import workflows and target measurements remain
tracked gaps in the full completion ledger. Do not activate production authentication
based on this milestone alone.
