# `web/src/entry/` - the bundles that are NOT the app

`cmd/webbuild` builds separate entry bundles for the dashboard and its independent browser documents:

| entry | document | format |
|---|---|---|
| `src/main.ts` | `index.html` - the dashboard | ESM module, deferred |
| `src/entry/login.ts` | `login.html` - the sign-in and first-run page | IIFE, classic |
| `src/entry/subscriber.ts` | isolated subscriber portal | IIFE, classic |
| `src/entry/subscriber-handoff.ts` | generated NAS HotSpot handoff files | IIFE, classic, at most 4096 bytes |
| `src/entry/preflight.ts` | the `<head>` of `index.html` | IIFE, **blocking** |

## Why they live apart

Not tidiness - the tooling asks questions that only make sense per document, and
answering them across all three at once produced four wrong answers the day these
files arrived:

- **`reachable-audit` and `module-reachability-audit`** walk the import graph from
  `main.ts`. An entry point is imported by nothing, so both reported these two as
  dead code. They now read the entry list out of `cmd/webbuild` and walk from each.
- **`wiring-audit`** asks "does the port touch every element the live app's page
  has". `login.html` has its own `#setupError`, and so does the ROUTER first-run
  overlay in `index.html` - the same id in two documents. Scanning `login.ts`
  alongside the app's modules made a recorded gap in the overlay look closed.
- **`class-hook-audit`** asks whether a toggled class is styled. `.visible` is
  defined in `login.html`'s own `<style>` block, not in `app.css`, so the app's
  stylesheet is the wrong place to look for it.

Each of those audits now scopes itself to one document, which is what it was
always asking about.

## Both were the live repo's files

`web/public/login.js` and `web/public/preflight.js` shipped verbatim from
`../MikroDash/public/` until 2026-08-28. The login-page check and
the preflight check drive the live file and this port's BUILT bundle
through the same stub DOM and compare operation for operation; that is what let
the copies be deleted rather than trusted.
