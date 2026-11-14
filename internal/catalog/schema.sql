-- Current v1.1 catalogue schema (user_version 9).
-- Existing v1 databases retain their data; older installs upgrade via Beta 2.
CREATE TABLE accounts (
 id INTEGER PRIMARY KEY, identity TEXT NOT NULL UNIQUE, provider TEXT NOT NULL,
 kind TEXT NOT NULL, secret BLOB NOT NULL, state TEXT NOT NULL DEFAULT 'unverified',
 connection_limit INTEGER NOT NULL DEFAULT 1 CHECK(connection_limit BETWEEN 1 AND 20),
 failures INTEGER NOT NULL DEFAULT 0, last_check INTEGER NOT NULL DEFAULT 0,
 next_check INTEGER NOT NULL DEFAULT 0, expires INTEGER NOT NULL DEFAULT 0,
 created INTEGER NOT NULL
, external_connections INTEGER NOT NULL DEFAULT 0, check_successes INTEGER NOT NULL DEFAULT 0, check_cycle INTEGER NOT NULL DEFAULT 0, rate_key TEXT NOT NULL DEFAULT '', catalogue_checked INTEGER NOT NULL DEFAULT 0, last_failure_kind TEXT NOT NULL DEFAULT '', last_failure_phase TEXT NOT NULL DEFAULT '', last_failure_status INTEGER NOT NULL DEFAULT 0);
CREATE TABLE channel_aliases (channel_id TEXT NOT NULL REFERENCES channels(id), alias TEXT NOT NULL, PRIMARY KEY(channel_id,alias));
CREATE TABLE channel_policies (
 channel_id TEXT PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
 target INTEGER NOT NULL DEFAULT 0, hours INTEGER NOT NULL DEFAULT 0,
 preference TEXT NOT NULL DEFAULT '', race INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE channels (
 id TEXT PRIMARY KEY, number TEXT NOT NULL, name TEXT NOT NULL,
 region TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1
, aliases TEXT NOT NULL DEFAULT '[]', profile_id TEXT NOT NULL DEFAULT 'primary' REFERENCES output_profiles(id) ON DELETE CASCADE, UNIQUE(profile_id,number));
CREATE TABLE events (id INTEGER PRIMARY KEY, happened INTEGER NOT NULL, kind TEXT NOT NULL, channel_id TEXT NOT NULL DEFAULT '', source_id INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT '');
CREATE TABLE import_jobs(id INTEGER PRIMARY KEY,started INTEGER NOT NULL,finished INTEGER NOT NULL DEFAULT 0,state TEXT NOT NULL,processed INTEGER NOT NULL DEFAULT 0,summary TEXT NOT NULL DEFAULT '');
CREATE TABLE mappings (
 channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
 stream_id INTEGER NOT NULL REFERENCES streams(id), enabled INTEGER NOT NULL DEFAULT 1, origin TEXT NOT NULL DEFAULT 'manual',
 PRIMARY KEY(channel_id,stream_id)
);
CREATE TABLE output_profiles (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL,
 market TEXT NOT NULL DEFAULT '', country TEXT NOT NULL DEFAULT '',
 timezone TEXT NOT NULL DEFAULT '', guide_location TEXT NOT NULL DEFAULT '',
 extensions TEXT NOT NULL DEFAULT '{}', enabled INTEGER NOT NULL DEFAULT 1, preset_id TEXT NOT NULL DEFAULT '', device_id TEXT NOT NULL DEFAULT '', port_offset INTEGER NOT NULL DEFAULT 0 UNIQUE
);
CREATE TABLE provenance (
 account_id INTEGER NOT NULL REFERENCES accounts(id), fingerprint TEXT NOT NULL,
 label TEXT NOT NULL, first_seen INTEGER NOT NULL, last_seen INTEGER NOT NULL,
 PRIMARY KEY(account_id,fingerprint)
);
CREATE TABLE provider_rates (provider TEXT PRIMARY KEY, next_api INTEGER NOT NULL DEFAULT 0, next_probe INTEGER NOT NULL DEFAULT 0, blocked_until INTEGER NOT NULL DEFAULT 0, failures INTEGER NOT NULL DEFAULT 0);
CREATE TABLE roster (
 channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
 stream_id INTEGER NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
 account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 admitted INTEGER NOT NULL, verified INTEGER NOT NULL, due INTEGER NOT NULL,
 PRIMARY KEY(channel_id,stream_id), UNIQUE(channel_id,account_id)
);
CREATE TABLE settings (name TEXT PRIMARY KEY,value TEXT NOT NULL);
CREATE TABLE streams (
 id INTEGER PRIMARY KEY, account_id INTEGER NOT NULL REFERENCES accounts(id),
 identity TEXT NOT NULL UNIQUE, name TEXT NOT NULL, tvg_id TEXT NOT NULL DEFAULT '',
 region TEXT NOT NULL DEFAULT '', secret BLOB NOT NULL, state TEXT NOT NULL DEFAULT 'unverified',
 successes INTEGER NOT NULL DEFAULT 0, failures INTEGER NOT NULL DEFAULT 0,
 consecutive_failures INTEGER NOT NULL DEFAULT 0, next_check INTEGER NOT NULL DEFAULT 0,
 last_check INTEGER NOT NULL DEFAULT 0, last_success INTEGER NOT NULL DEFAULT 0,
 score REAL NOT NULL DEFAULT 0.5, startup_ms REAL NOT NULL DEFAULT 0,
 playback_seconds REAL NOT NULL DEFAULT 0, stalls INTEGER NOT NULL DEFAULT 0,
 codec TEXT NOT NULL DEFAULT '', width INTEGER NOT NULL DEFAULT 0, height INTEGER NOT NULL DEFAULT 0,
 enabled INTEGER NOT NULL DEFAULT 1
, check_cycle INTEGER NOT NULL DEFAULT 0);
CREATE TABLE validations (id INTEGER PRIMARY KEY, stream_id INTEGER REFERENCES streams(id), checked INTEGER NOT NULL, success INTEGER NOT NULL, reason TEXT NOT NULL, startup_ms INTEGER NOT NULL, duration_ms INTEGER NOT NULL);
CREATE TABLE work_leases (
 kind TEXT NOT NULL, resource INTEGER NOT NULL, until INTEGER NOT NULL,
 PRIMARY KEY(kind,resource)
);
CREATE INDEX accounts_rate ON accounts(rate_key,check_cycle,id);
CREATE INDEX events_happened_kind ON events(happened,kind);
CREATE INDEX queue_account_due ON accounts(next_check,catalogue_checked,state,id);
CREATE INDEX queue_stream_due ON streams(next_check,state,account_id);
CREATE INDEX roster_due ON roster(due,channel_id);
CREATE INDEX roster_stream ON roster(stream_id);
CREATE INDEX streams_account ON streams(account_id);
CREATE INDEX streams_due ON streams(next_check);
CREATE INDEX validations_checked ON validations(checked,success);
INSERT INTO output_profiles(id,kind,name,preset_id,market,country,timezone,guide_location) VALUES('primary','hdhomerun','USA - New York City','us-nyc','New York City','US','America/New_York','10001');
CREATE VIEW active_channels AS SELECT c.* FROM channels c JOIN output_profiles p ON p.id=c.profile_id WHERE p.enabled=1;
