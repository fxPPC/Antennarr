# Validation

Run `go vet ./...` and `go test -race -p 1 -timeout 15m ./...`. Generate controlled media with `sh scripts/fixtures.sh /tmp/antennarr-fixtures`, then set `ANTENNARR_TEST_FIXTURES=/tmp/antennarr-fixtures` when running the suite. The `-p 1` option runs packages serially to reduce contention in timing-sensitive media tests; race detection stays enabled. Tests must not contact real providers. Docker permission smoke tests use isolated volumes and network-disabled containers; see packaging/README.md.

## Published-version upgrade and manual restore

Create a disposable fixture with the published v1.0.0 image:

```
python3 scripts/make-upgrade-fixture.py /tmp/antennarr-upgrade-fixture
ANTENNARR_UPGRADE_FIXTURE=/tmp/antennarr-upgrade-fixture go test ./internal/recovery -run TestPublishedV1UpgradeAndManualRestore -count=1 -v
```

The generator refuses an existing output directory, runs with networking disabled, and seeds only reserved `.invalid` provider addresses. The test copies the synthetic data, opens it with current code, verifies default settings and additive schema-8 metadata, lowers the roster threshold without dropping entries, and performs a manual backup/restore. It compares the exact HDHomeRun discovery, lineup, and device XML with the published version, along with accounts, rosters, login, random queue positions, and saved check-pass state.

## Matching regression and performance

Catalogue tests cover scoped imports, unchanged starts, channel-rule edits, ambiguity resolution, manual assignments, exclusions, learned TVG identities, and current preset labels. `go test ./internal/catalog -run '^$' -bench BenchmarkIncrementalImport -benchtime=1x` measures a one-stream import after seeding 10,000 streams and 143 selected channels. Setup is excluded from the timed result.

Long-duration playback and real Plex client recovery are separate acceptance checks. Automated fixtures or an HTTP connection remaining open do not establish uninterrupted household-client playback.

### Final-image media checks

Source tests alone do not verify the FFmpeg packaged in a release. After creating
synthetic fixtures, run `packaging/test-runtime-media.sh` with
`ANTENNARR_TEST_IMAGE` and an absolute `ANTENNARR_TEST_FIXTURES` directory. This
runs media, failover, multi-viewer, and CPU-affinity tests inside the finished
image, with a read-only filesystem and no external network. Native amd64 and
ARM64 jobs are both required; the existing combined required check reports
failure if either architecture fails.

Release publication checks every finding in the complete scan:
`python3 scripts/check-image-vulnerabilities.py REPORT.json --require-clean`.
This includes low-severity and unfixed findings. An explicit maintainer review in
`scripts/release-security-reviews.json` can approve exact findings for one release
using `--release-version VERSION`. Changed packages, severities, new findings, or
newly available fixes require another review. Reports are retained when a
candidate fails; an upstream advisory is a release blocker, not an excuse to
remove package metadata or suppress a result.
