# Unreleased

- Allow up to 4 MiB / five seconds of source analysis before codec-copy remuxing. Some valid broadcasts announce audio before sending its format details; the previous small probe could reject them immediately with no usable audio sample rate.
- Keep the existing source startup deadline, connection budgets, and no-video-transcoding policy. Add a synthetic delayed-audio regression test.

Schema 8, channel definitions, accounts, rosters, and tuner identity are unchanged. This fixes source initialisation; it does not identify programme content or guarantee every provider stays available.

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
