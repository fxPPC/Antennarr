# HTTP API

The main listener defaults to `:9192` and serves tuner routes plus authenticated administration routes. Setting `ANTENNARR_ADMIN_LISTEN=:9193` enables a separate administration listener that omits tuner discovery/playback. Administration requires HTTP Basic authentication; `/healthz` and `/branding/logo.png` are public exceptions. Keep the main listener private and use HTTPS for remote administration. See [listener and proxy setup](INSTALL.md#addresses-and-docker-networking). Administration mutations require `X-Antennarr-Request: 1`. Bodies are bounded, and errors omit credentials.

## Tuner

| Method / path | Purpose |
|---|---|
| GET `/discover.json`, `/device.xml` | Stable tuner identity and configured viewer limit |
| GET `/lineup.json`, `/lineup_status.json` | Enabled channels, stable stream URLs, and scan state |
| POST `/lineup.post` | Plex-compatible scan acknowledgement |
| GET `/auto/v{number}` | MPEG-TS session; viewers of a channel share its upstream pair |
| GET `/healthz` | Process liveness |

An additional viewer beyond the configured limit receives 503; an unknown channel receives 404. Media startup failures return 502/504. HTTP 200 starts when media arrives. HEAD requests do not allocate viewers. Keeping HTTP connected does not establish decoder continuity.

## Administration

| Method / path | Purpose |
|---|---|
| GET `/`, `/admin` | Web interface |
| GET `/api/status` | Viewer and channel-controller counts |
| GET `/api/monitor` | Playback, primary/backup feeds, work, coverage, queue stages, and saved check-pass progress |
| GET `/api/connection` | Addresses Plex can use |
| GET `/api/system` | Host, resource, and background/streaming load information |
| GET, POST `/api/login` | Current username/change login; current password required |
| GET, POST `/api/settings` | Settings; immediate changes and restart requirements are reported by the save response |
| GET, POST `/api/removal-policy` | `{account_failures,stream_failures}`, 0–100; zero disables that automatic retirement threshold |
| POST `/api/import` | `{text,authorised:true}`; list URLs, optionally Base64 encoded; starts asynchronous import |
| GET `/api/input/import` | Latest import state and result |
| GET `/api/input/accounts?q=...&offset=0` | Paginated, redacted accounts; search provider/status or account/stream IDs; `{items,total,offset,limit,has_next}` |
| GET `/api/output/search?q=...` | Redacted stream metadata and matching channel IDs; at most 100 stream detail rows, with `truncated` when more match |
| POST `/api/input/clear` | Confirmed removal of unassigned input accounts; active/rostered accounts protected |
| GET, POST `/api/channels` | List/save channels, permanent IDs, numbers, names, region, optional required station ID, enabled state, and aliases |
| DELETE `/api/channels/{id}` | Remove a channel |
| POST `/api/channels/bulk` | Confirmed clear-all or reset-to-US-preset operation |
| GET `/api/channels/{id}/excluded` | Excluded stream mappings |
| POST `/api/mappings` | `{channel_id,stream_id,enabled}`; explicit channel inclusion/exclusion |
| GET `/api/roster` | Channel coverage; add `?queue=1` for input queue totals |
| GET `/api/roster/{id}` | Verified stream memberships |
| POST `/api/roster/{id}/policy` | Channel overrides: `{target,preference,race}`; zero/empty inherits output settings |
| GET `/api/events` | Bounded recent events |
| POST `/api/outgoing/{id}/stop` | Stop a channel's outgoing viewers |
| POST `/api/backup` | Manual full application ZIP; returns download address |
| GET `/api/backups/{name}` | Download a manually created backup |
| POST `/api/restore` | Upload/validate/restore a full backup; requires idle playback |
| POST `/api/reset` | Confirmed application reset; requires idle playback |

### Operation payloads

Send JSON with `Content-Type: application/json` and `X-Antennarr-Request: 1`, plus Basic authentication:

| Operation | JSON body | Playback requirement |
| --- | --- | --- |
| POST `/api/input/clear` | `{"confirmed":true}` | Active/rostered accounts are protected |
| POST `/api/channels/bulk` — clear | `{"confirmed":true,"default_us":false}` | Idle |
| POST `/api/channels/bulk` — US preset | `{"confirmed":true,"default_us":true}` | Idle |
| POST `/api/reset` | `{"confirmed":true}` | Idle; restarts the application |
| POST `/api/lineup` | `{"confirmed":true,"lineup":{"version":1,"channels":[...]}}` | Idle; replaces channels and rebuilds rosters |

Lineup JSON is limited to 2 MiB and 1,000 channels. Export `GET /api/lineup` for the complete portable structure; each channel carries `id`, `number`, `name`, `region`, optional `station`, `enabled`, `aliases`, and `policy` (`target`, `preference`, `race`). IDs and numbers must be unique. Policy zero/empty values inherit output settings. `station` is an optional case-insensitive station identifier (up to 32 letters, digits, dots, or hyphens); empty removes the station requirement. Channel edits retain the permanent ID and re-evaluate affected source assignments.

POST `/api/restore` uses **multipart/form-data**, with the ZIP in a file field named `backup`; it is not JSON and needs no `confirmed` field. Keep the request header and Basic authentication. The ZIP limit is 1 GiB, with at most 2 GiB of expanded files. Playback must be idle. Success returns `restoring`, `restart_required`, and the saved `tuner_url`, then restarts the application.

POST `/api/backup` needs the request header and authentication, but no JSON body. It returns `backup` (the archive name) and `download` (an authenticated relative download URL). Existing backup archives and Docker deployment configuration are not included.

Conflicting maintenance operations or non-idle playback where required return 409. Confirmation is an explicit request field, not a substitute for authentication. Backups contain secrets and are only created manually. Restoring the same tuner identity, channels, and reachable URL preserves Antennarr's side of Plex registration; Plex's own database is separate.

## Matching and checking

Aliases and exclusions ignore case. An alias beginning with `-`, with optional following whitespace, excludes a whole word or contiguous phrase; exclusions override positive names and learned TV identifiers. Region distinctions remain significant. UHD detection includes joined names such as `ESPNUHD`.

Check passes persist random stream order and per-account access validation. Provider pacing keeps work pending. Failed accounts release their roster memberships and re-enter the random input queue; the configured failure threshold retires them. A successful usable-account check resets the streak. HTTP 429 and cancellation do not count as account failures. Retired rows are physically deleted after their reservations are released; active accounts are protected. `check_pass.released` counts snapshot memberships removed for replacement, separately from completed media checks.


## Search navigation

Use `account:42` or `stream:42` (also `account #42` / `stream #42`) for an exact ID of one kind. Plain numbers can match either account or stream IDs; Output also matches channel numbers. Text matches are case-insensitive for ASCII names, and `%`/`_` are literal characters. Queries are limited to 120 bytes. Search never opens sources or returns credential fields.

UI URLs retain filters: `#input?q=account%3A42`, `#input?q=provider&offset=50`, and `#output?q=stream%3A42`. An absent explicit ID displays a removed/unavailable message. Output includes saved candidates outside rosters and can expand matching channels; detail results are capped, but all matching channel IDs are returned.

Full valid accounts release roster places and return to the queue without gaining a failure. The former `count_full_connections_as_failures` setting is no longer exposed or honoured; old saved configuration still loads. Antennarr-owned live connections do not count as external occupancy. Roster targets govern admission only: reducing global or channel targets preserves existing eligible members.

Large imports commit in bounded batches, yielding to playback health writes. Malformed endpoints are rejected before any batch is committed. If a later database operation fails, its batch rolls back and earlier batches remain; retrying deduplicates existing entries.

## v1.0.0 additions

- `GET /api/lineup`: download version-1 channel JSON; no accounts or rosters.
- `POST /api/lineup`: `{ "confirmed": true, "lineup": { "version": 1, "channels": [...] } }`; 2 MiB maximum, strict portable fields, idle playback required. Replacement is atomic; sources/settings/device identity remain.
- Settings: `replenish_while_playing` (boolean, default false); `logging_level` (`none`, `normal`, `debug`, default debug). Logging applies immediately and persists.
- Monitor `database_error` describes a delayed summary without hiding live state. Check-pass `deferred` counts capacity deferrals separately from released roster memberships.
