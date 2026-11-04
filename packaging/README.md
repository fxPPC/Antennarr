# Public image release

`Dockerfile.release` accepts a deliberately small context:

- `binaries/antennarr-arm64` and `binaries/antennarr-amd64`: static, trimpath Go builds of cmd/antennarr.
- `tests/{catalog,config,admin,recovery,tuner,roster,activity}-{arm64,amd64}.test`: platform-specific Go test executables.
- `packaging/entrypoint.sh`: root setup and non-root launch wrapper.
- `LICENSE`: Antennarr GPLv3 licence text.
- `source.tar.gz`: clean corresponding source, made with `sh packaging/source-bundle.sh /absolute/source.tar.gz`.
- `licenses/`: project-bundled third-party notices, Go SDK licence, and dependency licences.

Build and push with Buildx for linux/arm64,linux/amd64, passing VERSION and REVISION. The verification stage runs catalogue/configuration/admin/recovery/tuner/roster/activity tests and checks FFmpeg/FFprobe on each target. It is never the shipping base: the final stage derives from clean runtime and copies only a verification receipt. No test executable, generated test credentials, data directory, server configuration, key, or Docker login belongs in the final context layers.

Use temporary Docker credentials, keep build work at lower CPU priority than Plex, and remove the dedicated builder/cache/credentials after publication. Do not change the production container merely to push a tag. A separately authorised public brand route serves only the embedded logo; administrative assets and tuner endpoints remain protected.

Public repository overview: docs/DOCKER-HUB.md. Antennarr uses SPDX identifier `GPL-3.0-or-later`. Include LICENSE and retain dependency notices. Distributed binaries/images must be accompanied by corresponding source as required by the licence.

Permission smoke tests: `python3 packaging/test-permissions.py`, with `ANTENNARR_TEST_IMAGE` optionally naming a local candidate image. They use an isolated disposable volume and network-disabled containers, testing default/custom IDs, migration, explicit non-root launch, limited capabilities, and error messages.
