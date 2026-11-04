# Licence inventory

Antennarr's application licence is the root LICENSE: GPL-3.0-or-later.

Bundled Go binary notices:

- GOLANG.txt and GOLANG-PATENTS.txt: Go standard library/toolchain notices.
- GOLANG-X-TEXT.txt and UNICODE-CLDR.txt: golang.org/x/text and Unicode material.
- modernc-sqlite-LICENSE: modernc.org/sqlite's BSD licence.
- modernc-THIRD-PARTY.md: upstream transitive dependency inventory, including golang.org/x/sys and the indirect modules used by SQLite. Its relative links refer to the upstream repository; they do not change Antennarr's application licence.

Debian packages, including FFmpeg, retain their installed notices under /usr/share/doc in the runtime image. Before a public release, verify this inventory against the exact Go module graph and installed Debian package versions, and publish the corresponding source/retrieval information with that release. This inventory is not a substitute for checking the actual licence terms.
