# v1.0.5

- Refresh Debian runtime packages during image builds and pull current base images for release candidates.
- Bring the recent Go dependency updates into the stable branch: SQLite, libc, x/sys, and x/text. Update Docker login tooling.
- Keep the stable branch covered by daily grouped Dependabot updates and checked automatic merges.
- Scan the actual runtime image in CI and on both release architectures. Preserve the complete reports and block publication when high or critical findings have an available fix.

This remains the single-tuner, schema-8 maintenance line. The UK/multi-tuner PR is integrated separately into 1.1. Existing channels, accounts, settings, and tuner identity are preserved. No automatic backup is created.

Package scans can still report issues for which Debian or another upstream has not supplied a fix. Remaining findings must be reported; a successful release gate does not mean zero vulnerabilities.
