# Releases

Stable releases use `vMAJOR.MINOR.PATCH` tags and the same Docker Hub version. A published version is never replaced; fixes receive a new patch version. `latest` follows the most recent stable release. Historical beta tags remain available for compatibility.

## Checks and candidates

Source checks run on pushes and pull requests using GitHub-hosted runners. Controlled media fixtures and disposable upgrade data never use live provider accounts. Fork checks receive no publication secrets.

Run **Actions → Approved release** from `main`, entering a new version. The workflow runs the full source/media/upgrade checks, then builds native Linux amd64 and ARM64 candidates. It tests image permissions and fresh defaults, generates SPDX inventories, and records candidate manifest digests. Candidates are uploaded as short-lived workflow artifacts; they are not public Docker tags yet.

## Required approval

The `release` environment must require `fxPPC` as reviewer, allow self-approval, and restrict deployments to `main`. Its only Docker secrets are `DOCKERHUB_USERNAME` and a read/write `DOCKERHUB_TOKEN` without delete rights. Store them in the environment, not repository-wide secrets. The personal Docker account currently exposes account-level repository permissions, so this token can write repositories managed by that account; it is not repository-scoped. It expires after 90 days and can be replaced in this environment. Do not use a personal password.

Review the checks and candidate digests before approving the publication job. Approval publishes those exact candidates by digest, creates the multi-platform version and `latest` tags, records GitHub build provenance, and publishes a GitHub Release with source, checksums, per-platform SPDX files, and image digest. It does not rebuild the approved candidates or update a running installation.

Enable immutable GitHub releases before launch. Assets are attached while the release is a draft, then published. Protect `v*` tags against updates/deletion; allow the release maintainer/automation to create new tags.

## Recovery and verification

Before publication, confirm version/tag absence and ensure the candidate source is reviewed. The workflow rechecks before writing. If publication fails after uploading some blobs or a version tag, inspect the run and registry before retrying; do not overwrite a published stable release. Keep a draft release unpublished until all required assets and provenance exist.

After publication, verify both platform manifests and matching version/latest digests; pull by digest and test fresh startup. Confirm source revision labels, embedded source, defaults, and zero accounts. Record actual test coverage, including any unverified Plex-client or multi-hour viewing scenarios.

Changing runtime appdata is separate from publishing. Backups remain manual. Never include runtime data, host operations, Docker credentials, test executables, or private development history in the public repository or final image.
