# Web tests

The frontend's own tests. They bundle the app's TypeScript with esbuild and run it
under `node --test`, against the DOM shim in `dom-shim.ts`.

```
npm test          # from web/
```

They moved here when the port-parity harness in `tools/` was retired. These are
the ones that survived, because each drives THIS app and asserts what it does -
none of them compares the rendering against a recording of the implementation
this one replaced.

## They are executed, not type-checked

`web/tsconfig.json` covers `src/`, not this directory, and that is deliberate.

These tests substitute a fake DOM for the real one. Type-checking that
substitution against `lib.dom` is a category error: every error it produces is
"your stand-in node is not an `HTMLElement`", which is the entire point of a
stand-in. Silencing that would take roughly a hundred `as unknown as HTMLElement`
casts, which assert exactly what the shim already knows and would hide a real
mismatch if one ever appeared.

Nothing is lost relative to how these ran before: esbuild strips types without
checking them, so the harness was never type-checked either. What checks them is
that they RUN - and they run the app's real modules, so a type error that matters
shows up as a failure rather than a warning.

`tsc --noEmit` still covers every module they import, because those live in
`src/`.


## Upstream optional-feature browser acceptance

With Chromium, `playwright` and `@axe-core/playwright` installed:

```sh
NODE_PATH=/usr/lib/node_modules node web/test/browser/optional-features-journey.cjs
```

Run from the repository root. This starts a loopback HTTP fixture, bundles the real
DNS, Wi-Fi, CAPsMAN, Settings and Domain Routing modules, and serves their actual
markup and styles. All endpoint responses and socket payloads are synthetic;
RouterOS writes are never sent. It checks disabled defaults, keyboard interactions,
375/1280 layouts and dark/light/Nord palettes, fails on axe violations, and prints
the temporary directory containing screenshots and per-view axe results. It is an
explicit browser acceptance command, separate from `npm test` and the default CI.
