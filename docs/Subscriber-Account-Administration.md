# Reviewed subscriber status changes

Subscriber Management provides literal username/display-name search and an
account-status filter over its existing protected account list. Filtering never
renames or translates data. The main badge remains the total account count;
the list states the number currently shown. Search is local to that complete
response, without per-keystroke HTTP or NAS requests. Large-fleet pagination and
load measurements are separate work.

Select individual rows, enter the new status and a change reason, then choose
**Review selected subscribers**. The review freezes up to 50 account identities,
their current statuses, revision tokens, the new status and the reason. Check
the explicit confirmation before applying. Changing filters, clearing selection
or refreshing cancels the old review. A row's Suspend/Reactivate action opens the
same review flow for that account; it does not immediately change its state.

Busy controls prevent repeated submissions. Input survives errors; a failed or
uncertain request requires a fresh account read and a new review. Success appears
only after the backend commits and responds. The server remains authoritative if
a client bypasses these controls.

## HTTP contract

Existing `system:subscribers` modern-administrator authorization, same-origin
`Origin` and `X-MikroDash-Request: subscriber-admin` checks apply. The subscriber
listener exposes none of this API. Existing individual `status` requests remain
compatible; the shipped account table uses the reviewed batch action.

`GET /api/subscribers` returns each account's `statusRevision` as a decimal JSON
string. Initial status has revision `"0"`. A status write through the existing
individual action or a committed batch advances the corresponding audit identity.
Display-name edits do not invalidate a status-only review. A status changed and
then restored still invalidates the earlier revision.

Example with synthetic identifiers only:

```json
{
  "action": "status-batch",
  "status": "suspended",
  "reason": "Reviewed account suspension",
  "items": [
    {
      "accountId": "synthetic-account-id",
      "expectedStatus": "active",
      "expectedRevision": "0"
    }
  ]
}
```

`status` is `active` or `suspended`. Every item needs a unique account ID, its
reviewed status and the non-null decimal revision string. Numeric JSON revisions
are refused to avoid JavaScript precision loss. The batch contains 1–50 accounts;
the actor and a valid Unicode reason of at most 2048 UTF-8 bytes are mandatory.

A committed response contains `batchId`, `changed` account rows with their new
revisions, a `skipped` count and ordered `results` for every reviewed item. Each
result has an `account` row and an `outcome` of `changed` or `unchanged`; an
unchanged account retains its revision. The UI displays each original username
with its committed outcome. It clears previous results and success feedback while
a new request is pending. A conflict or uncertain response marks every reviewed
item unconfirmed and requires a fresh read. It does not infer successful or failed
storage from a lost response. Already matching statuses are skipped without
fabricating another change/audit. The correlation ID is not an authentication
credential or an idempotency token. A stale retry is refused rather than silently
reusing an old review; after a lost response, read the current state before trying
again.

Missing accounts, changed statuses or changed revisions return HTTP 409 and roll
back the entire selection. Invalid input returns 400; existing authorization and
Origin failures remain 401/403. Audit or database failures cannot return committed
success. An account at the end of a batch can invalidate every earlier update.

## Storage and enforcement

Conditional updates, revision comparisons and per-account before/after audit
records share one SQLite transaction. Audit records use `account.status.batch`
and one common batch ID. The indexed status-only audit lookup detects a changed
then restored status without an additional account database or mutable history.
Schema 44 adds only the idempotent partial index `subscriber_status_revision`;
it changes no stored identity, credential, subscription, reservation or usage.
Subscriber audit IDs advance explicitly inside the transaction. At the signed
64-bit maximum, audited writes fail and roll back instead of accepting SQLite's
random smaller rowid fallback. The last representable revision remains readable;
old review tokens cannot become current again. Do not delete audit history or
restore an older balance database to work around an exhausted identity range.
See [SQLite rowid allocation](https://www.sqlite.org/autoinc.html).

Suspension blocks new admission and reaches the existing independently running,
guarded enforcement worker. This admin action adds no RouterOS API write path
and grants no additional CoA/Disconnect authority. A Disconnect ACK does not
settle missing accounting or release its unresolved credit. Portal authentication
remains available under the existing account policy. Reactivation changes status
only; it does not renew a subscription or restore spent quota.

No operator runtime was restarted or migrated for this development. Before a
later approved rollout, preserve the normal `/data` backup/compatibility process.
An older binary that rejects schema 44 must not be pointed at the updated data.
Use a compatible rollback binary; do not delete version/audit rows or restore an
old database to force a downgrade and erase subsequent consumption. No router
rollback is needed because this development changes no operator router setting.

## Acceptance

SQLite tests cover stale and changed-then-restored reviews, overlapping
concurrent batches, failure on a later row, audit failure, empty/duplicate/oversized
input, 50 valid accounts, Unicode, voucher/password preservation and revisions
above JavaScript's safe integer limit. The migration test reconstructs schema 43,
verifies the real indexed query plan and proves idempotence and retained audit/
credentials. Existing connection migration tests retain active usage and leases.

The real protected HTTP/SQLite Chromium journey covers filtering, cancellation,
changed-then-restored conflict, immutable review, delayed genuine commit response,
busy controls, reactivation, keyboard focus and escaped data. It exercises the
current dark/light/Nord themes at 375/1280 with axe and screenshots. A real
authenticated loopback UDP test proves committed suspension reaches guarded
background Disconnect with every browser absent. NAS observation in that test
is synthetic; this does not claim target C session disruption or acceptance.
