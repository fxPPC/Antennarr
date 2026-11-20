# v1.1.0-beta.1

## What changes when you upgrade from 1.0

- **Run several Plex tuners from one installation.** Each tuner has its own Plex address, channel lineup, and playback quality preference. Choose from New York City, London, Mexico City, Mumbai, Berlin, and Amsterdam. Your existing tuner keeps its identity, channel edits, and rosters.
- **Give channels with the fewest feeds first pick.** Antennarr spreads working feeds across more source accounts and can still use an account that supplies other channels when alternatives are scarce. Channel matching distinguishes local editions and time-shifted feeds.
- **Pause background scans while keeping TV available.** Turn off **Scans active** under **Sources** to keep playing channels from their existing rosters. The setting survives restarts.

**Before upgrading**, make a manual backup of your 1.0 data and try the beta on a separate copy. The beta upgrades the database to schema 9. Returning to 1.0 requires the pre-upgrade backup. Multi-tuner guide mapping and recordings with overlapping channel numbers still need testing in Plex.

Stable 1.0 remains the default. To try the beta on Linux amd64 or ARM64, pull `antennarr/antennarr:v1.1.0-beta.1`. [Multi-tuner setup](https://github.com/fxPPC/Antennarr/blob/v1.1.0-beta.1/docs/MULTI-TUNER.md)

# v1.1.0-alpha.8

## Changes since v1.0.x

- A single installation can serve several Plex tuners, each with its own guide, channel numbers, channel rules, and rosters. New York City and London are available as editable presets. You can also run with one tuner or none.
- Fresh installs start with zero tuners, channels, and accounts. First-time setup lets you choose a market. Adding a preset creates a disabled tuner so you can check its guide and address before enabling it.
- The **Scans active** switch in Sources pauses background checks and saves your choice across restarts. Existing rosters remain available for playback.
- Status links each coverage count to the affected channels. Open a tuner to search and edit its channels or find its Plex address. Sources shows how many accounts an import added and how many were already present.
- Each tuner has a single preferred playback quality setting: Automatic, 720p, 1080p, or UHD (4K). All tuners share the outgoing stream limit. Viewers watching the same channel share its upstream feeds.
- Opening a 1.0 data directory upgrades it from schema 8 to 9. The primary tuner keeps its identity, channel edits, and rosters. Existing 1.1 installations reopen their saved lineup. Make a manual backup before upgrading if you need a route back to 1.0.

The ARM64 Docker image is `antennarr/antennarr:v1.1.0-alpha.8`. Stable tags `1.0` and `latest` point to v1.0.7. Multi-market Plex recordings with overlapping channel numbers still need wider testing.

# v1.1.0-alpha.7

- Output counts channels across all saved tuners in one query. Plex reads only the selected tuner’s enabled lineup.
- Zero, one, and multiple tuners are supported. Removing the final tuner keeps administration available after restart.
- Bare Mac WebKit previews get a lightweight entry page, preserving the request allowance for the dashboard.
- The 1.1 beta format baseline remains schema 9.

# v1.1.0-alpha.6

- Preserve playback failure strikes while history writes are busy.
- Count catalogue reserves across channels in one local query.
- Choose Automatic, 720p, or 1080p under each tuner in Output. Other resolutions can still serve as fallbacks.
- Honour provider HTTP 503 responses as deferrals without striking accounts. Malformed authentication responses still count.

# v1.1.0-alpha.5

Missing or unrecognised provider authentication flags are reported as malformed responses. They count towards the configured consecutive account failure limit.

# v1.1.0-alpha.4

Full accounts wait an hour between discovery retries. Roster checks continue across available providers while others are blocked. New installations default to 15 streams per channel.

# v1.1.0-alpha.3 — GitHub preview

- Bring the v1.0.6 catalogue, HLS playback, guarded demuxing, and credential-safe media diagnostic fixes into the multi-tuner branch.
- Refresh the container runtime and run native amd64/ARM64 media and permission tests against its packaged FFmpeg.
- Test upgrades and manual restore from published v1.0.0 and v1.0.4 images, preserving user-edited channels and tuner identities.
- Keep the independent US and London tuners, London matching rules, and existing 1.1 data format unchanged.

GitHub prerelease only. Docker Hub and Slander remain on the stable 1.0 series. Use separate test appdata; returning to 1.0 requires a manually created pre-upgrade backup.

---

# v1.1.0-alpha.2 — GitHub source preview

- Add independently enabled **USA - New York City** and **UK - London** tuners. London has 110 editable channels, informed by 22 provider catalogues and current broadcaster listings, with conservative region, service, timeshift, and sister-channel exclusions.
- Give each tuner a stable identity and private listening port. The existing primary address stays unchanged; additional devices use a configurable `9200–9207` range. Channel numbers may overlap between tuners.
- Add tuner cards, enable checkboxes, scoped editing, roster counts, lineup import/export, reset, removal, and preset addition under Output. Mobile layouts and cross-tuner search are supported.
- Share account budgets, provider pacing, and the global outgoing-stream limit. Viewers of the same channel share upstream work; different market channels remain separate.
- Upgrade schema 8 transactionally to schema 9, preserving primary channel IDs, edits, mappings, rosters, and device identity. Add London disabled once; fresh installations start both tuners disabled. Removing a tuner does not make it reappear on restart.
- Tighten country parsing around channel brands such as 5USA and Al Jazeera. Reject uncertain cross-market matches and contradictory regional identifiers.

**Distribution:** GitHub only. Docker Hub and existing 1.0 installations are unchanged. Build this source against separate disposable appdata. Returning from schema 9 to 1.0 requires a manual pre-upgrade backup. No backup is created automatically.

**Still to verify:** actual Plex registration with independent US/UK guides, overlapping-number recordings, and long-duration multi-market client playback. Controlled routing, sharing, migration, and media tests do not establish those behaviours. See [London metadata review](UK-LONDON.md), [port setup](MULTI-TUNER.md), and [remaining release gates](V1.1-PLAN.md).

---

# v1.1.0-alpha.1 — unreleased development foundation

- Include all v1.0.4 channel-matching, preset, and delayed-audio startup fixes. Preserve the v1.1.0-alpha.1 identity and release line.
- Add a published-v1.0.4 upgrade and backup/restore regression check, including edited and disabled custom channels. Define **USA - New York City** as the only initial, optional tuner preset; additional locations follow later.
- Start the v1.1 development line, with multiple independently configurable Plex tuners and same-minor stable-patch downgrades as the release goals.
- Define the compatibility boundary: preserve `/data` on minor upgrades; restore a pre-upgrade backup to return from 1.1 to 1.0. The new downgrade guarantee starts at stable 1.1.0.
- Add branch/version validation, opt-in prerelease publishing, stable minor Docker tags, and explicit latest promotion. Maintenance releases cannot replace a newer stable version as latest.

Multi-tuner runtime support, the new database migration, and the stable-patch downgrade harness are not implemented in this foundation. No new schema is applied yet. Development/prerelease builds do not carry the stable downgrade guarantee. See [the implementation plan](V1.1-PLAN.md).

---

# v1.0.4

- Review all 150 US channel definitions and expand provider aliases for 27 channels, including numbered New York affiliates, Investigation Discovery, ESPN, American Heroes, and legacy premium-network names. Keep existing guide numbers and station requirements.
- Reject conflicting regional and service metadata more consistently: compact guide IDs now respect exclusions such as `-pacific` and `-latin america`, and dotted country suffixes distinguish feeds such as `.in` from `.us`. A conflicting affiliate ID cannot override the required station in the name.
- Recognise additional unrelated guide services and reject catalogue section headings. Ambiguous feeds stay unassigned; missing sources are preferable to the wrong programme.
- Handle provider `VIP` quality decoration and bracketed `[BK]` backup markers without relaxing complete-name matching or the UHD setting.
- Allow more input probing time and bytes for valid MPEG-TS feeds whose AAC audio arrives late, fixing a startup failure seen with NBC. Video is still copied; startup deadlines remain bounded.

Schema 8, account history, channel IDs, and tuner identity are retained. Saved streams are rematched once; contradictory memberships are removed without failure strikes. Existing channel definitions are not overwritten. The improved preset is used for fresh installs or an explicit reset. Backups remain manual. Metadata checks cannot prove programme content or uninterrupted playback across every provider and Plex client.

---

# v1.0.3

- Build with Go 1.27.4, including the HTTP server security fix for GO-2026-6859.
- Reject conflicting recognised stream names and guide IDs during import and rematching, even when the guide ID has never been learned. Ambiguous identities stay unassigned; manual assignments cannot bypass exclusions or station constraints.
- Add an optional **Required station ID** to channel editing, lineup export/import, and manual backups. Primary and hot-backup feeds must identify that station. Changing the rule removes incompatible roster memberships without adding failure strikes.
- Fresh installs include **150 editable US channels**, adding seven broadcast channels with New York station requirements. Existing channel definitions are preserved; missing channels can be added individually, or the US preset can be reset explicitly.
- Correct country-tag parsing for `The CW`, `(The CW)`, and bracketed station labels. Discovery and saved assignments share the same ambiguity checks.

Schema 8, existing channel IDs, accounts, and tuner identity are retained. Saved feed assignments are re-evaluated once under the corrected matching rules. Recognising station metadata cannot prove programme content or detect provider holding screens; check the picture and guide before scheduling recordings. Controlled tests do not establish uninterrupted playback across every provider and Plex client.

---

# v1.0.2

- Full accounts release their roster places and return to the random input queue without gaining or resetting strikes. Their accounts and catalogue remain available for later auditions. Antennarr's own live feeds are excluded from external occupancy. The old option to count full connections as failures has been removed, including when loading an older saved configuration.
- Cached UHD rosters cannot restore streams evicted for account capacity. A new successful media audition is required.
- New roster check passes prioritise the oldest verification, with random ties. Input accounts and retry queue positions remain random; checks still run in continuous passes and yield to playback.
- Browser API requests have deadlines, allowing status refreshes to recover from a hanging request. Read requests time out after 15 seconds; mutations allow three minutes and are never retried automatically.
- Docker logs identify unexpected pipeline termination and slow-viewer disconnections without revealing source credentials. Media probes distinguish early endings, process failures, invalid transport, missing timestamp progression, and stalled samples.
- Backup creation releases the administration lock after capturing its input files, before ZIP compression. Snapshot failures and completed staging clean up temporary files. Backups remain manual.

Schema 8 is unchanged. Existing channels, accounts, login, and tuner identity are preserved. Full accounts already saved in rosters are evaluated on their next access check, not removed based solely on old metadata. Controlled tests cover recovery and sharing; they do not establish uninterrupted playback across every provider and Plex client.

---

# v1.0.1

- Dashboard statistics refresh in the background, keeping live activity responsive. Loading counts are explicit, saved counts remain visible during failures, and read snapshots no longer reserve SQLite's writer lock.
- Automatic discovery stops building extra reserves for full rosters; one matching candidate per queued account remains. Idle cleanup bounds saved alternatives to three roster targets (minimum 60 per channel and quality class), retaining provider diversity, current rosters, explicit assignments, and reserved sources. One matching candidate per queued account is retained for future recruitment.
- Bulk reserve trimming produces one summary event instead of flooding removal history. SQLite temporary sorting uses RAM to avoid exhausting small container temporary filesystems. Database failure codes are logged without credential-bearing error text.
- Full backups support up to 1 GiB compressed / 2 GiB expanded. Restore uploads stream to appdata staging instead of filling RAM-backed temporary storage. Backup failures are reported in Docker logs.
- Update modernc.org/sqlite from 1.61.0 to 1.61.1.

Schema 8 is retained. Existing accounts, channels, settings, and tuner identity are preserved. Excess unrostered automatic candidates may be removed during idle cleanup and rediscovered later; rostered/manual/reserved sources are protected. SQLite reuses freed pages, so an existing database file may remain large until separately compacted. Backups remain manual. Reverse proxies must allow the chosen restore upload size.

---

# v1.0.0

- Restore extraction uses a filesystem-rooted staging handle as well as ZIP-name validation. Provider connection counts are bounded before integer conversion so oversized occupancy remains full instead of wrapping negative.

- Verified roster entries survive unused startup-race cancellation and temporary account capacity limits. Actual media/account failures and explicit channel edits retain their removal policies.
- Optional playing-channel replenishment fills active-channel vacancies while routine checks and idle-channel recruitment remain paused. Connection budgets and provider pacing still apply.
- Status distinguishes queued reserves from runnable work when rosters are full, and shows provider retry windows instead of a false stalled-checker warning.
- Status eligibility starts from vacancies and pending work, avoiding repeated whole-catalogue scans. A failed summary refresh retains live activity and shows a warning instead of blanking the page.
- Channel lineup JSON export/import includes names, numbers, country, aliases/exclusions, enabled state, and per-channel policies. Import atomically replaces channels and rebuilds rosters, retaining accounts, settings, and tuner identity.
- Admin offers immediate, persistent None / Normal (warnings and errors) / Debug logging. Debug records safe capacity, source lifecycle, failure type, and media-check diagnostics.
- Catalogue size/response bounds defer discovery without counting an account failure. Retained false failure streaks from Beta 4 catalogue limits are repaired once. Full accounts remain in the random input queue without failure strikes by default; our own live connections are excluded from external occupancy.
- Compact count formatting supports K/M/B/T units while APIs, IDs, settings, and pagination retain exact values.
- Quality-mode roster restoration preserves verified surplus above a subsequently lowered target.

The v1 data format remains schema 8. Existing data and channel identities are retained. Backups remain manual. Pre-v1/schema-1–7 installations must first upgrade through Beta 2.
