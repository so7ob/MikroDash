# Test evidence

Snapshot: 2026-11-13 (Asia/Aden). Baseline `ba39515f003484211afe3dfef783ade6dc39f813` on `develop`; `main` = `ca2b1d5284d2da98967a5bc482282ee85d02bc8e`. Issue #95, parent #38. Source inspection is not an executed test or physical-device acceptance.

Tests listed in inventory exist in source; that alone is not a pass. Historical PR claims and CHR reports are provenance, not fresh target verification.

| Revision | Command / evidence | Actual result |
|---|---|---|
| ba39515f | go test ./internal/subscriber ./internal/server ./internal/guard | PASS: subscriber 69.748s; server 128.914s; guard 0.225s (synthetic SQLite/HTTP/UDP tests, no physical NAS) |
| ba39515f | git status, fetch/pull --ff-only, ls-remote, branch ancestry | develop current; main ancestor; no voucher ref found |
| GitHub current settings | develop protection API | strict Actions `test` (app 15368), resolved conversations, admins enforced; zero mandatory approvals |
| PR #77 old head | PR body cites Actions 40759795136 | Historical; not reusable for current develop base |
| Physical RouterOS / L009 C | No live test run this session | Pending Hardware Verification |
| Synthetic load 100/1k/10k/100k | Not run | No latency/RSS/CPU/router-channel claim |

For each implementation revision record the exact SHA, command, pass/fail/skip and artifact/CI URL. Full `tools/verify.sh`, `git diff --check`, production build and required current PR/post-merge CI precede delivery. Tagged browser tests require explicit `subscriberbrowser` and browser tooling; standard go test does not execute them.

| bdc31ae39378ea2eceaa6ec777a1714a96956339 | Clean detached worktree: sh tools/verify.sh | PASS: 64 Go packages, govulncheck, generators, tsc, 7 table generators, 156 web, 32 tooling; /tmp/mikrodash-77-clean-verify.log |
| Same head | go run ./cmd/webbuild -dir web; tagged TestSubscriberBrowserAcceptance and HandoffBrowser | PASS, 119.556s; /tmp/mikrodash-77-browser.log and /tmp/mikrodash-77-browser-evidence; synthetic NAS |
| Same head/current ba39515f base | [PR CI 43307893327](https://github.com/so7ob/MikroDash/actions/runs/43307893327) | PASS including image/startup smoke |
| Visible checkout | sh tools/verify.sh | FAIL on ignored .temp/activity-82-upgrade/check.go format; no scratch file altered; clean identical-SHA worktree passed full gate |
| 149123b76a882b6811cf6052b5c34d1d7461afb3 | #77 squash merge | Merged; post-merge CI pending |

Additional fresh evidence: #77 post-merge [43308821709](https://github.com/so7ob/MikroDash/actions/runs/43308821709) PASS; #78 head 293bcb6a full verify PASS (64 Go packages, 159 web/32 tooling), six editor browser/axe cases and real HTTP/SQLite plan regression PASS; PR CI 43309860407 and post-merge 43310757862 PASS. #80 head 6e7fcfcf full verify PASS (64 packages, 159 web/32 tooling); PR CI 43311070342 and post-merge 43312107877 PASS. Synthetic scale results/CPU/RSS and limits are preserved in docs/Subscriber-Status-History.md.

Retained failures: #78 initial current-base CI 43309347585 rejected stale 115/116 frontend-file documentation; corrected to 116 and new-head gates passed. #81 first genuine browser run failed a harness expectation of decoded HTML characters inside raw JSON; the corrected assertion and actual HTTP/SQLite journey passed at 2089d59d (114.921s). Visual review led to readable batch reasons and retained original disclosure at f6c63712; its fresh final checks are pending. A first #79 fuzz build was interrupted by an agent branch switch (missing source file), not a product failure; rerun in an isolated fixed-SHA worktree passed 16597 cases in 5.066s, and focused preview tests/vet passed. No failed run is counted as a pass.
