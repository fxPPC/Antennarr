# v1.0.1

- Dashboard statistics refresh in the background, keeping live activity responsive. Loading counts are explicit, saved counts remain visible during failures, and read snapshots no longer reserve SQLite's writer lock.
- Automatic discovery stops saving candidates for full rosters. Idle cleanup bounds saved alternatives to three roster targets (minimum 60 per channel), retaining provider diversity, current rosters, explicit assignments, and reserved sources. Accounts remain available for rediscovery.
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
