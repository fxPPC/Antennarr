# v1.0.0

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
