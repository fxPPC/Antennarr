# Licence inventory

Antennarr's application licence is the root LICENSE: GPL-3.0-or-later.

Bundled Go binary notices:

- GOLANG.txt and GOLANG-PATENTS.txt: Go standard library/toolchain notices.
- GOLANG-X-TEXT.txt and UNICODE-CLDR.txt: golang.org/x/text and Unicode material.
- modernc-sqlite-LICENSE: modernc.org/sqlite's BSD licence.
- modernc-THIRD-PARTY.md: upstream transitive dependency inventory, including golang.org/x/sys and the indirect modules used by SQLite. The opening licence link points to our bundled SQLite licence; other upstream path references describe that dependency, not Antennarr. This does not change Antennarr's application licence.

The candidate runtime uses Wolfi packages, including FFmpeg. Package licence declarations, exact versions, and source references remain in APK metadata and the generated release SBOM. Upstream package recipes, patches, and source commits are at https://github.com/wolfi-dev/os; FFmpeg's corresponding source recipe is `ffmpeg-9.0.yaml`. Retain the matching recipe and source references with a release, and verify its full licence inventory before publication. This inventory is not a substitute for checking the actual licence terms.
