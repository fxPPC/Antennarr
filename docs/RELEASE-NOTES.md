# v1.0.9

Docker now checks the configured tuner listener. Automatic Plex discovery uses the configured address and port when no public base URL is set. This fixes incorrect discovery URLs and false unhealthy reports on custom ports.

Rapid dashboard reloads now have a separate allowance for bundled UI files, keeping tuner edits and status requests available.

The Linux amd64 and ARM64 runtime release checks permit exactly two unfixed findings in glibc 2.44-r24: CVE-2026-115819 (medium) and CVE-2026-100290 (low). An available fix or any different finding blocks publication.

# v1.0.8

Stable maintenance release for Linux amd64 and ARM64. Updates the Wolfi runtime base and CI tooling. Existing 1.0 settings, channels, rosters, and Plex tuner identities use the same schema.

# v1.0.7

Full accounts wait an hour between discovery retries. Roster checks continue across available providers while others are blocked. New installations default to 15 streams per channel.

# v1.0.6

- Decode large Xtream catalogues incrementally, retaining the 40,000-entry limit and using a separate 64 MiB catalogue byte limit. Reject incomplete/non-array catalogues without accepting a partial list.
- Repair HLS gateway links so FFmpeg accepts both transport-stream and fragmented-MP4 segments. Preserve private playlist references during bounded URL recycling.
- Restrict source demuxing to supported TV formats; keep external URLs behind the guarded gateway.
- Preserve upstream HTTP failure codes and log fixed, credential-free media failure categories with bounded memory.
- Use a digest-pinned Wolfi runtime with current FFmpeg and GNU permission tools; retain package metadata and full scanner reports.
- Test the finished container's media pipeline on native amd64 and ARM64, including shared viewers, failover, and background CPU confinement. Remove the unused alternate Dockerfile.

Existing 1.0 data remains compatible. No automatic backup or roster reset is introduced.

# v1.0.5

- Refresh Debian runtime packages during image builds and pull current base images for release candidates.
- Bring the recent Go dependency updates into the stable branch: SQLite, libc, x/sys, and x/text. Update Docker login tooling.
- Keep the stable branch covered by daily grouped Dependabot updates and checked automatic merges.
- Scan the actual runtime image in CI and on both release architectures. Preserve the complete reports and block publication when high or critical findings have an available fix.

This remains the single-tuner, schema-8 maintenance line. The UK/multi-tuner PR is integrated separately into 1.1. Existing channels, accounts, settings, and tuner identity are preserved. No automatic backup is created.

Package scans can still report issues for which Debian or another upstream has not supplied a fix. Remaining findings must be reported; a successful release gate does not mean zero vulnerabilities.
