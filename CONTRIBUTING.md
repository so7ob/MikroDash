# Contributing to MikroDash

Thanks for your interest in contributing. Small changes are as welcome as large ones - typo fixes, documentation, and a single-line bug fix all count.

## Development and delivery policy

This is the delivery policy for **so7ob/MikroDash**. It applies to humans and agents,
including fixes, features, documentation, dependencies, CI, refactors and maintenance.
Read [CLAUDE.md](CLAUDE.md) for the repository's technical and operational constraints.

**Every change:** Issue → branch from `develop` → implementation and tests → PR to
`develop` → verified merge. Split unrelated changes into separate issues and branches.

| Branch | Purpose | Allowed delivery |
| --- | --- | --- |
| `develop` | Integration of completed, tested work | PRs from issue branches; release synchronization from `main` |
| `main` | Stable release line and default branch | Release PRs from `develop`, only on an explicit release/promotion request |
| `<type>/<issue-number>-<description>` | One issue's work | Created from current `origin/develop`, removed after delivery |

Use lowercase, hyphenated descriptions and a Conventional Commits type: `feat`, `fix`,
`docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore` or `revert`.
For example, `fix/42-router-reconnect` and `chore/43-prepare-release`.
Commit subjects and PR titles use `<type>(optional-scope): <description>`; keep subjects
within 72 characters and put issue references in the body/footer.

### Authorization and boundaries

A request to implement development authorizes its Issue, branch, commits, branch pushes,
PR and merge into `develop` after the checks below pass. Related fixes needed to finish
that same task use the same authorization. Audit or planning requests authorize no writes.
Do not ask again for routine delivery steps already covered by the implementation request.

Promotion to `main`, pushing a version tag and publishing a release require an explicit
request for that promotion/release. A release request covers its preparation, promotion,
tag, Docker publication and GitHub Release. It does not authorize deploying the application,
writing to routers or deleting operator data. An earlier release request does not authorize
the next release. “Package it up” alone means local version/changelog preparation and a
commit on an issue branch; it does not request promotion or publication.

### Issue first, then branch

1. Verify the checkout and `origin` identify `so7ob/MikroDash`; inspect local changes.
   Preserve unrelated work, including locally installed agent skills. Use explicit file
   paths when staging; never sweep unrelated files into a commit.
2. Search this repository's issues before creating one. Every implementation needs an
   issue with its problem/goal, acceptance criteria, scope and verification plan. Reuse an
   existing issue for the same work. Report sensitive vulnerabilities through
   [SECURITY.md](SECURITY.md), without publishing exploit details or credentials.
3. Fetch `origin`, update local `develop` with `git pull --ff-only origin develop`, and
   create the numbered issue branch from it. Never commit work directly to `develop` or
   `main`. Do not reset or discard a dirty checkout to switch branches.
4. Implement the issue, append to `Changes.md` after every file edit, and run the checks.
   Include a regression for a bug and test the relevant call site. Never remove or weaken
   checks to make a change pass. Keep version bumps and release notes for release preparation.

Use `--repo so7ob/MikroDash` for GitHub commands: this repository is a fork, and the upstream
repository must not receive its issues or PRs accidentally.

## Development Setup

```sh
git clone https://github.com/so7ob/MikroDash.git
cd MikroDash
```

You need **Go 1.27+**. **Node 24** is needed only to type-check and test the frontend: the frontend itself is built by a Go program, and nothing Node-related runs at runtime.

```sh
go run ./cmd/webbuild -dir web                    # build the TypeScript frontend into web/dist
go build ./cmd/mikrodash                          # the binary
./mikrodash -data ./devdata -web web/dist -static web/public
```

The dashboard is then at <http://localhost:3082> (`-listen` changes the address).

If you would rather not install a Go toolchain, the Go commands also run in a container:

```sh
docker run --rm -v "$PWD":/src -w /src golang:1.27-alpine sh -c "go vet ./... && go build ./..."
```

**You do not need a MikroTik router to contribute.** MikroDash starts without one and shows the setup wizard, so frontend, documentation and test work need nothing but the toolchain. A reachable RouterOS device is only required to see live data.

## Running the checks

```sh
(cd web && npm ci)   # once: TypeScript and esbuild for the frontend checks
sh tools/verify.sh   # everything: gofmt, vet, go test, govulncheck, generated code, tsc, web tests
```

`tools/verify.sh` is the one to run before opening a PR. Its Go half runs in a `golang` container, so it needs Docker; without Docker it says what it skipped, and a skipped check is not a completed verification. CI runs the Go checks explicitly before
using `--no-docker` for the remaining checks.

It **discovers** what to check rather than working from a list, so a new check runs without being registered anywhere:

| | |
|---|---|
| `internal/verify/` | 113 Go tests - static checks over the current source. Picked up by `go test ./...`. |
| `web/test/` | 107 test files that bundle the app's TypeScript and run it against a DOM shim, via `npm test` in `web/`. |

Package tests use the standard library `testing` package only.

## Project Conventions

These are deliberate constraints rather than style preferences:

- **Fewer router channels.** Concurrent API channels, not data volume or CPU, are what strain small hardware, so "more efficient" means asking the router for less. Each menu is read once however many collectors want it, and every collector supports both stream and poll delivery, chosen per router.
- **Every WebSocket event is declared with its payload type** (`hub.Declare`), and the browser's types are generated from those declarations by `cmd/tsgen`. A payload that is a Go map is typed in `web/src/events-hand.ts`, and `tsc` fails if that file and the declarations disagree about which events those are.
- **Generated code is never edited by hand.** `web/src/gen/` comes from `cmd/tsgen`, `cmd/pagesgen` and `tools/*-ts.js`; change the source and regenerate. The recordings under `testdata/` pin what the app does today, so change them deliberately, never by retyping one.
- **A check that cannot fail is worse than no check.** Anything that scans a set asserts it actually found something. An audit that silently measures zero reads exactly like one that passed.
- **A gap is recorded, never hidden.** The ledgers in `internal/verify/` fail in both directions: an unrecorded gap fails, and so does a recorded one that has since closed.
- **Self-hosted assets.** Everything the browser loads lives in `web/public/vendor/`, so the dashboard works on an isolated network with no internet access. No CDN references.
- **A small dependency footprint.** There are eight Go dependencies and each has a reason beyond convenience. `esbuild` is used through its Go API, which is why building the frontend needs no JavaScript runtime. New ones are worth discussing first.
- **Errors are sanitised.** Anything reaching the browser goes through `safe.Message()` first.
- **Deliberate changes are welcome; silent ones are not.** If your change alters what a page shows, a WebSocket payload or an interaction, say so in the PR. If it makes a check fail, update the check and explain why in the commit - do not delete it.

The collector layer - how data is read from the router, turned into payloads and sent to the pages that want it - is described in **[Collector-Architecture.md](docs/Collector-Architecture.md)**. You do not need to read it before starting: copying the closest existing collector in `internal/collect/` is a perfectly good way to begin.

## Pull requests and completion

1. Run `sh tools/verify.sh` and `git diff --check`. Record commands, outcomes, omissions and
   the tested SHA in the PR; exercise changed behavior as well as the unit tests. For workflow
   changes, validate Actions syntax and verify the actual GitHub settings/runs.
2. Push only the issue branch, then open the PR explicitly against `develop` in
   `so7ob/MikroDash`. Link the issue, explain the final behavior and include validation evidence.
3. Review the final diff and resolve review conversations. An agent's own review is a
   self-review, not independent approval. No additional human approving review is mandatory
   by default; any blocking review still needs resolution.
4. Wait for the required `test` check from GitHub Actions on the current PR revision and
   current base. Both long-lived branches require PRs, up-to-date checks and resolved
   conversations, including for administrators; force pushes and branch deletion are disabled.
   Do not use an admin bypass or weaken protection. A pending, failed, skipped or stale check
   is not sufficient evidence, even when GitHub would permit a skipped check.
5. **Squash merge** an issue branch into `develop` using its Conventional Commit PR title.
   Verify the resulting `develop` SHA and wait for its post-merge Tests run to succeed.
6. Explicitly close the issue with the PR link, merge SHA and CI evidence after delivery.
   GitHub closing keywords only auto-close on the default branch, which remains `main`.
   Release issues stay open until the complete release procedure below finishes.
7. Delete only the merged issue branch, synchronize local `develop` using fast-forward
   only, and confirm `HEAD` equals `origin/develop`. Report Issue/PR links, SHA and pre/post-merge
   checks. Preserve pre-existing local files; do not describe such a checkout as wholly clean.

If post-merge CI fails, keep the issue open, report the failure and deliver the correction
through another PR associated with that issue. Do not promote an unverified `develop`.

## Releases, only when requested

1. Create a release issue identifying the requested version and included work. Unless a
   different version is requested, use the next patch version. Create a `chore/<issue>-prepare-release`
   branch from `develop`, update `AppVersion` and `CHANGELOG.md`, and check the release-facing
   documentation. One bump covers the release; normal development does not bump the version.
2. Deliver preparation through a tested PR into `develop`. Keep the release issue open.
3. Open a PR **from `develop` to `main`**, referencing the release issue. Recheck the request
   and the current source/base, require passing CI, and use **merge commit**, not squash or
   rebase, to preserve ancestry. Keep both long-lived branches.
4. Wait for all checks on the resulting `main` commit, including the multi-architecture
   validation build. Create an annotated `vX.Y.Z` tag on that exact verified SHA and push only
   that tag. Never move or overwrite an existing release tag.
5. The tag workflow verifies that the tag belongs to `main`, matches `AppVersion`, and has
   one nonempty matching CHANGELOG entry before any publication. It reruns the existing suite,
   publishes `ghcr.io/so7ob/mikrodash` for `linux/amd64`, `linux/arm64` and `linux/arm/v7`, then
   creates the GitHub Release from those notes. Main pushes and manual branch validation do
   not publish an image or a release.
6. Verify the tag target, successful publishing run, image tags/digest/platforms and GitHub
   Release. A build, a merge, an image publication and a deployment are separate outcomes.
   If publication fails, inspect the existing tag/image/release before retrying; never blindly
   repeat a mutation or retag another commit.
7. Synchronize `main` back into `develop` through a PR linked to the same release issue,
   using **merge commit** so the next release retains ancestry. Wait for PR and post-merge CI,
   update local `develop` and close the release issue with the evidence. Do not delete `main`
   or `develop`, and do not push their synchronization directly.

## Reporting Bugs

Use the [bug report template](https://github.com/so7ob/MikroDash/issues/new?template=bug_report.yml). Router model and RouterOS version help a lot, since behaviour varies between versions.

For security vulnerabilities, please follow [SECURITY.md](SECURITY.md) instead of opening a public issue.
