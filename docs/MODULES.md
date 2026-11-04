# Module boundaries

The application is a modular monolith. `cmd/antennarr` is the composition root: it creates services and wires narrow callbacks/interfaces. Packages must not reach into Docker, Portainer, or host-specific operational files.

- `ingest`: untrusted list/page parsing and Xtream metadata adapters. Produces normalised `Entry` records; never opens a viewing session. `Resolver.ResolveLists` is the current manual-input adapter. A future RSS adapter should produce the same entries and provenance, not another checker.
- `catalog`: SQLite persistence and current-schema initialisation, channel identity/matching, roster admission, scheduling leases, and saved health. Credentials stay inside this boundary except for explicit authenticated use.
- `roster`: serial background scheduling. `service.go` selects work; the catalogue persists unique-stream traversal and per-pass access checks in `check_pass.go`; `checks.go` performs account/catalogue/media checks; `recruit.go` preserves account locality; `health.go` reports safe state. Reuses provider pacing and account budgets. Live playback preempts checks.
- `engine`: primary/ghost selection, live health, replacement, and source switching through `Catalogue`, `CopyFunc`, `InspectFunc`, and `Output` interfaces. It owns one channel's upstream pair, not one pair per viewer.
- `media`: URL-confined FFmpeg gateway, probing, transport analysis, and codec-copy packaging. It has no UI or Plex configuration responsibility.
- `stream`: shared subscriptions, viewer limits, cancellation, and stop controls. Cancelling one shared channel cannot corrupt another's counts.
- `tuner`: current HDHomeRun HTTP adapter. A future output requests shared channel sessions from `stream`; it does not duplicate `engine`.
- `monitor`: read-only collection boundary. Caches a consistent SQLite read snapshot for five seconds; live playback/work state is sampled on every response. It never causes checks or opens sources.
- `systeminfo`: bounded host diagnostics and in-memory load samples, classified by actual streaming/checking activity. No subprocess arguments come from requests, and idle time is not presented as background-check load.
- `admin`: authenticated HTTP/UI, validated settings, and explicit operations. It delegates work rather than implementing alternate media pipelines.
- `netguard`, `moderation`, `budget`, `checkcpu`, `access`: shared security and resource policies. New inputs/outputs must not bypass them.
- `recovery`: bounded, validated, explicit manual backups/restores, and staged factory reset before SQLite opens. Reset retains manual archives. No automatic backups.

## Future-compatible data

Configuration has `format_version`, a stable `profile_id` (`primary`), and an `extensions` object preserved through saves. Unsupported future versions fail rather than being silently rewritten. SQLite user version 8 adds `output_profiles` and `output_assignments`; the primary assignment mirrors existing channel edits. Accounts and canonical channel IDs remain shared. Profiles can retain market, country, time zone, guide location, and extension data without changing tuner URLs.

v1 still enforces one active profile and globally unique channel numbers. A future multi-profile migration must lift that constraint, route profile-specific endpoints, and prove Plex guide behaviour. The current schema is a foundation, not a claim that multiple tuners already work. Existing DeviceID remains authoritative in configuration; it is preserved when opening existing v1 data.

Roster checking uses continuous saved passes. Legacy hours/due fields remain for format compatibility, but no longer schedule roster checks. Complete discovery failures share the configured account deletion policy across worker and manual-refresh paths.
