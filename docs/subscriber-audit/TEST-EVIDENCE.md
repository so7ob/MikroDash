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
