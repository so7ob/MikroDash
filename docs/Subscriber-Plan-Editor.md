# Draft concurrent plan limits

The Plans and policies tab can add, edit and remove limits before saving an
immutable plan version. Each draft limit retains its ID when edited; removing
one never renumbers or reuses its ID. Cancel leaves the previously saved draft
limit unchanged. These actions do not write to the server. Existing subscriptions
and saved plan versions are unchanged until the normal separate assignment flow.

Choose time, download, upload or total bytes; a reset period; an IANA time zone;
and an RFC3339 anchor with an explicit offset. The anchor is an instant; the
selected zone determines its calendar interpretation. A disconnect limit still
operates concurrently with every other configured limit. Throttle exposes positive
download and upload ceilings in bit/s using the existing backend policy action.
This editor does not prove live NAS throttle capability or enable network writes.

Amounts and throttle rates remain decimal strings, including above JavaScript's
safe integer range. Leading zeros are normalized before submission. Negative,
fractional, exponent, non-decimal and signed-64-bit overflowing values are rejected
without changing the saved draft. Calendar/backend validation remains authoritative.

Finish or cancel an open limit edit before saving the plan version. While a save
is pending, all plan inputs/actions are disabled and repeat submissions are ignored.
The request contains a detached snapshot of the draft. Failure or an interrupted
response retains the draft and reports an unconfirmed save: refresh the plan list
before retrying. A lost response does not prove rollback; immutable version checks
on the server still protect existing saved plans. A confirmed API response precedes
success feedback. Refreshing the browser discards this in-memory draft.

This is a bounded editor for the existing single-policy form. Full saved-plan
loading, multi-policy/window editors, custom speed menus, credential/history/setup
administration and transactional import remain outside this increment and open
under #38. #76 hardware acceptance remains pending.

## Verification

Run the regular frontend typecheck/tests. The synthetic browser acceptance uses
actual shipped modules and CSS with a local HTTP mock, not the SQLite handlers:

```sh
SUBSCRIBER_BROWSER_MODULE=/path/to/browser-dependencies \
SUBSCRIBER_CHROMIUM=/path/to/chromium \
SUBSCRIBER_EVIDENCE_DIR=/tmp/plan-editor-evidence \
node tools/subscriber-plan-browser.mjs
```

The dependency directory contains `playwright` and `@axe-core/playwright`.
The harness covers edit/cancel/remove, invalid edits, exact submitted quantities,
stable identities, a partially emitted/disconnected response, a server refusal,
repeated pending submissions, a deliberate retry, keyboard focus, escaped Unicode,
three existing palettes at 375/1280 widths, screenshots and axe WCAG 2 A/AA plus
2.1 AA tags. Local acceptance passed these six combinations with zero axe findings,
and TypeScript plus all 154 frontend tests passed. This does not claim persisted
SQLite transactions, live quotas or router acceptance. Local Go integration remains
blocked by the pinned SQLite archive's Forbidden download; required CI is separate.

Two harness setup failures were retained during development: terminating a response
before any bytes allowed Chromium transport retry, and axe required an explicit
browser context. The fixture now interrupts a partially emitted JSON body and uses
an explicit context; neither change relaxes product assertions.
