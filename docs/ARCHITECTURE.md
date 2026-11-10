# Architecture and limits

A modular Go monolith owns HTTP administration, the HDHomeRun adapter, SQLite/WAL catalogue, random input queue, continuous check passes, provider pacing, connection budgets, and shared channel controllers. See MODULES.md for package boundaries. Browser code is plain JavaScript. Runtime media tools are FFmpeg and FFprobe; no TSDuck or GStreamer dependency is shipped.

## Catalogue and checks

Only selected channels receive media checks. Imports match new/changed stream metadata. Channel-rule changes identify affected streams, including name ambiguities and learned TVG IDs. Automatic mappings are rebuilt for those streams; explicit decisions persist unless an exclusion rule forbids the stream. Startup refreshes matching only when its version changes. Bounded import batches yield to live health writes; each batch keeps metadata and mappings atomic. SQLite transactions also keep removal notices and totals consistent with deletion.

Each channel maintains a configurable verified roster. Targets are refill thresholds, not hard retention caps. Excess entries remain checked until removed naturally. Recruitment stops at the threshold. Checks traverse saved passes, prioritising the oldest verification and randomising ties. Imports and retries get random queue positions. A pass completes before another begins; there is no fixed check interval. Playback preempts routine background work. Optional replenishment auditions only playing-channel vacancies, with successful admissions scoped to active outputs. Provider pacing, account/global budgets, and the last two available checking CPUs still apply.

Full valid accounts release all roster memberships and return to the random input queue without adding or resetting failure strikes. Their saved catalogue remains available, but a fresh successful media audition is required before readmission. The one-hour UHD cache cannot bypass this. Existing full accounts are handled on their next access check; stale occupancy alone does not trigger bulk removal on upgrade. Provider occupancy excludes locally reserved live feeds. Local catalogue-size bounds defer discovery without strikes. Other failed account/catalogue attempts follow the configured deletion threshold. Typed diagnostics expose fixed categories and status codes, not response bodies or credentials. Last-failure metadata remains on the account until success or deletion. Removal notices retain only IDs, provider hostnames, fixed explanations, and counts, within the existing 10,000-ID/seven-day event bound. Validation history is bounded to 50,000 ID positions. Totals may outlive detailed notices; the UI reports that gap.

## Live output

Viewers of the same channel share one controller. Startup races eligible candidates within account/provider/global budgets. The first valid source becomes primary; another continuously receiving source becomes the hot backup when available. A faltering backup is replaced; primary failure promotes a ready backup and replenishes it. Each viewer has a bounded downstream queue; slow clients are disconnected independently. The last viewer leaving starts a brief grace period before upstream cleanup.

External media uses DNS/IP-validated clients and a loopback-only FFmpeg gateway. Video is copied; final audio is AAC stereo at 48 kHz and 128 kbit/s, with one audio encoder thread per channel. No video transcoding is enabled. RAM buffers and subprocess/connection budgets are bounded. Plex priority is a host deployment choice, not a built-in Docker dependency.

TS packaging repairs continuity counters and handles source timestamp discontinuities. Sources can differ in timing, resolution, codec, and programme parameters. Compatible-source recovery is tested with controlled fixtures; codec changes can still interrupt Plex decoding. An open HTTP connection does not prove client playback survived. Long-duration and household-client tests are separate from automated fixture results.

## State and recovery

All persistent state is under /data. Config format/profile identities and SQLite schema 8 support additive metadata while preserving tuner/channel identity. v1 exposes one tuner profile. Backups/restores are manual, bounded, validated, and include application state; Plex state is external. Startup integrity failure refuses the database rather than replacing it. See INSTALL.md for upgrades and recovery.

## Bounded candidate reserves

Automatic catalogue discovery saves additional reserves for enabled channels below their roster target, while retaining one matching candidate per queued account. Saved alternatives are limited to three times the target, with a minimum of 60 per channel and quality class. Idle cleanup retains a provider-diverse reserve ranked by score and recent success. Current roster members, explicit manual assignments, and reserved/leased sources remain protected even above that limit. One matching candidate per queued account is retained so healthy accounts do not appear unmatched merely because other accounts fill the reserves. Explicit imports are trimmed by the same idle policy unless manually assigned.

SQLite reuses freed pages; the file size does not immediately shrink after rows are deleted. Offline compaction is a separate maintenance operation.
