CREATE TABLE guide_feeds (
 profile_id TEXT PRIMARY KEY REFERENCES output_profiles(id) ON DELETE CASCADE,
 url TEXT NOT NULL
);
CREATE TABLE guide_channels (
 channel_id TEXT PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
 pin TEXT NOT NULL DEFAULT '',
 snapshot TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE guide_stations (
 profile_id TEXT NOT NULL REFERENCES output_profiles(id) ON DELETE CASCADE,
 id TEXT NOT NULL,
 station TEXT NOT NULL,
 name TEXT NOT NULL,
 sample TEXT NOT NULL,
 PRIMARY KEY(profile_id,id)
);
