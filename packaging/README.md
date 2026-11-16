# Container packaging

The root [Dockerfile](../Dockerfile) builds the application and the runtime image. Official publication uses the manually approved [release workflow](../.github/workflows/release.yml); see [release instructions](../docs/RELEASING.md).

## What goes into the image

The Go build stage runs tests, compiles a static application binary, and creates an allowlisted corresponding-source archive with `packaging/source-bundle.sh`. The candidate runtime uses the public, digest-pinned Wolfi base, with FFmpeg 9, FFprobe, CA certificates, curl, tini, and the util-linux-misc/bins packages. GNU ownership tools preserve the existing PUID/PGID and Docker --user behaviour. Package metadata is retained for scanners. It copies the application, source archive, and licence notices; it does not copy the Go toolchain or test executables.

The image contains no runtime accounts, playable stream URLs, appdata, private server configuration, or Docker credentials. `.dockerignore` excludes runtime and private files from the build context. The source archive has a separate allowlist. Keep both lists current when adding project files.

`VERSION` and `REVISION` build arguments set image labels. They do not rewrite application or UI version strings. Official releases build Linux amd64 and ARM64 candidates on native GitHub-hosted runners, test their permissions and fresh defaults, generate SPDX inventories, and publish the approved candidates without rebuilding them.

## Local build and permission tests

Run from the repository root:

```sh
docker build -t antennarr:permissions-test .
ANTENNARR_TEST_IMAGE=antennarr:permissions-test python3 packaging/test-permissions.py
```

These tests create and remove disposable containers and a dedicated volume. They test default/custom IDs, existing-data ownership, explicit non-root launch, limited capabilities, and startup errors. Test containers have networking disabled. Use a development Docker engine, not one where matching test container/volume names belong to another workload.

The entrypoint prepares `/data` ownership when started as root, then runs the application as UID/GID 10001 by default. `PUID`/`PGID` select other non-zero IDs. Explicit Docker `--user UID:GID` skips ownership setup; that user must already have access to appdata. See [installation](../docs/INSTALL.md).

## Source and licences

The image embeds corresponding source at `/usr/share/doc/antennarr/source.tar.gz`, Antennarr's GPL-3.0-or-later licence, and bundled notices under `/usr/share/doc/antennarr/licenses`. Runtime package licences and source references are recorded in APK metadata and the release SBOM; upstream build recipes are available at https://github.com/wolfi-dev/os. Retain attribution and check the licence inventory whenever dependencies change.

The Docker Hub overview is maintained in [docs/DOCKER-HUB.md](../docs/DOCKER-HUB.md). Publishing an image does not update any running installation.

Run `ANTENNARR_TEST_IMAGE=antennarr:test ANTENNARR_TEST_FIXTURES=/absolute/fixtures sh packaging/test-runtime-media.sh` after building. It tests the actual runtime media binaries using synthetic inputs, with no external network or appdata.

Publication requires a clean vulnerability report at every severity. The complete reports are retained even when the gate fails. A clean scan is a dated observation, not a guarantee against undiscovered vulnerabilities.
