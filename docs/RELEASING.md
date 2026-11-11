# Releases

Releases follow [the versioning and compatibility policy](VERSIONING.md). Stable releases use `vMAJOR.MINOR.PATCH`; opt-in test releases append `-alpha.N`, `-beta.N`, or `-rc.N`. Exact Git and Docker tags are immutable. Stable releases advance their minor Docker tag (`1.0`, `1.1`, etc.); only explicit promotion advances `latest` and GitHub's recommended release. Historical beta tags remain available.

## Checks and candidates

Before starting a new version, update the startup version in `cmd/antennarr/main.go`, the UI footer in `internal/admin/index.html`, Dockerfile version defaults, and release notes together. Set `RELEASE_LINE` to the intended major/minor line. Preflight rejects a version that disagrees with the source, line, branch, or published history; it does not rewrite application source or UI branding.

Source checks run on pushes and pull requests using GitHub-hosted runners. Controlled media fixtures and disposable upgrade data never use live provider accounts. Fork checks receive no publication secrets.

Run **Actions → Approved release** from `main` or the matching `release/MAJOR.MINOR` maintenance branch, entering a new version. Prereleases come from `main`. Enable **promote_latest** only for the recommended stable release; leave it off for test builds and older maintenance lines. A later 1.0 patch cannot replace stable 1.1 as latest. The workflow runs the full source/media/upgrade checks, then builds native Linux amd64 and ARM64 candidates. It tests image permissions and fresh defaults, generates SPDX inventories, and records candidate manifest digests. Candidates are uploaded as short-lived workflow artifacts; they are not public Docker tags yet.

## Required approval

The `release` environment must require `fxPPC` as reviewer, allow self-approval, and restrict deployments to `main` and explicitly supported maintenance branches (initially `release/1.0`). Keep the existing reviewers and secret scopes when adding a branch. Repository environment rules are configured separately from this workflow; verify them before a maintenance release. Its only Docker secrets are `DOCKERHUB_USERNAME` and a read/write `DOCKERHUB_TOKEN` without delete rights. Store them in the environment, not repository-wide secrets. The personal Docker account currently exposes account-level repository permissions, so this token can write repositories managed by that account; it is not repository-scoped. It expires after 90 days and can be replaced in this environment. Do not use a personal password.

Review the checks and candidate digests before approving the publication job. Approval publishes those exact candidates by digest, creates the multi-platform version and permitted moving tags, records GitHub build provenance, and publishes a GitHub Release with source, checksums, per-platform SPDX files, and image digest. Prereleases are marked as such on GitHub and never touch stable tags. It does not rebuild the approved candidates or update a running installation.

Enable immutable GitHub releases before launch. Assets are attached while the release is a draft, then published. Protect `v*` tags against updates/deletion; allow the release maintainer/automation to create new tags.

## Recovery and verification

Before publication, confirm version/tag absence and ensure the candidate source is reviewed. The workflow rechecks before writing. If publication fails after uploading some blobs or a version tag, inspect the run and registry before retrying; do not overwrite a published stable release. Keep a draft release unpublished until all required assets and provenance exist.

After publication, verify both platform manifests and matching digests for every tag intentionally updated; pull by digest and test fresh startup. Confirm source revision labels, embedded source, defaults, and zero accounts. Record actual test coverage, including any unverified Plex-client or multi-hour viewing scenarios. Before each stable 1.1 patch, also complete the same-minor downgrade matrix described in VERSIONING.md; the existing Beta 3 upgrade test does not establish that guarantee.

Changing runtime appdata is separate from publishing. Backups remain manual. Never include runtime data, host operations, Docker credentials, test executables, or private development history in the public repository or final image.

After a release is promoted to latest, the workflow synchronises `docs/DOCKER-HUB.md` to the existing Docker Hub overview and verifies the saved text. Maintenance and prerelease publications leave the stable overview alone. This uses the release environment credentials only. Check that step's result: documentation failure does not undo or republish an immutable image. Correct the overview separately if Docker Hub rejects the update.
