# Contributing to Antennarr

Bug fixes, clearer documentation, interface improvements, and controlled tests are welcome. Contributions are made under **GPL-3.0-or-later**; no CLA or DCO sign-off is required. Preserve third-party notices and identify the source and licence of reused code.

## Find the right place

Use [Issues](https://github.com/fxPPC/Antennarr/issues) for reproducible bugs and agreed work. Use [Discussions](https://github.com/fxPPC/Antennarr/discussions) for setup help and feature ideas. Report security issues privately through the [Security tab](https://github.com/fxPPC/Antennarr/security/advisories/new).

Never post provider credentials, account lists, full backups, live databases, private addresses, Plex tokens, recordings, or unredacted logs. Tests must use generated media and reserved test addresses, not public IPTV providers.

## Build and test

Install the Go version used by the Dockerfile, Node.js 24 or later for UI tests, and FFmpeg/FFprobe for media tests. Docker with Compose is required for image, permissions, and published-version upgrade checks.

```sh
go build ./cmd/antennarr
go vet ./...
node --test scripts/ui.test.mjs
sh scripts/fixtures.sh /tmp/antennarr-fixtures
ANTENNARR_TEST_FIXTURES=/tmp/antennarr-fixtures go test -race -p 1 -timeout 15m ./...
docker compose -f compose.portable.yaml up -d --build
```

See [testing](docs/TESTING.md) for upgrade/restore and permission checks. Keep disposable appdata separate from any real installation. Backups are explicit manual operations.

## Work on the UI separately

The web server is built into the Go executable. Its HTML/CSS and plain JavaScript live under `internal/admin`; there is no frontend framework, npm dependency tree, or separate web server to maintain. Start a disposable local application, edit the embedded assets, then rebuild/restart to see changes. `node --test scripts/ui.test.mjs` checks pure display helpers. For UI changes, test desktop and narrow mobile layouts, keyboard access, loading/error states, and sanitised screenshots.

## Design and style

Read [module boundaries](docs/MODULES.md), [architecture](docs/ARCHITECTURE.md), and the [API](docs/API.md). Keep services behind their existing boundaries; the admin layer delegates work. Explain non-obvious invariants and cancellation/ownership rules in comments, rather than repeating what each line does.

Reliability takes precedence over picture quality. Preserve codec-copy video, shared upstreams, live hot backups, provider pacing, connection budgets, and playback priority. Keep persistent state under `/data`. Do not introduce host-specific paths or dependencies on Docker/Portainer control APIs.

Use `gofmt`, British English in UI/docs, and small changes with focused tests. Include failure/cancellation cases for scheduling or streaming changes. Keep unrelated refactors out of bug fixes. Document codec/protocol limitations; do not claim seamless Plex playback from HTTP-only tests.

## Submit a pull request

Branch from `main` for v1.1 work or `release/1.0` for compatible 1.0 fixes. Carry relevant maintenance fixes forward into `main`; keep new features out of the maintenance branch. Follow the [versioning contract](docs/VERSIONING.md) and [v1.1 implementation plan](docs/V1.1-PLAN.md). Explain the problem and resulting behaviour, and record what was actually tested. Update relevant documentation and include screenshots for visible changes. Discuss substantial architecture or data-format changes first. Maintainers squash merges; release publication is a separate, manually approved action.

Dependabot updates targeting `main` automatically enable squash auto-merge, including major-version updates. The existing branch rules still require an up-to-date branch, source/media checks, and CodeQL before merging. After `main` changes, the workflow asks Dependabot to rebase queued PRs that have fallen behind, triggering fresh checks. Failed checks require repair; the automation never bypasses them. It handles PR metadata only and never runs dependency code with write permissions. Builds from `main` include updates after they merge; published versions remain immutable, and release publication stays manual.
