<img src="https://raw.githubusercontent.com/fxPPC/Antennarr/main/docs/images/logo.png" alt="Antennarr logo" width="160">

# Antennarr

**Stick your antenna into the Internet**

**v1.0.2 · 10 November build · by fxPPC**

An IPTV-to-Plex tuner I'm building because apparently watching television needed another software project. Import account-list URLs (plain or Base64), choose your channels, and let it maintain a roster of checked feeds. Viewers share a primary stream and live backup; failed feeds get replaced automatically, mid-session, (hopefully) without interrupting playback. That's the idea. Still testing the awkward bits.

Go, SQLite, and FFmpeg on Debian 13. ARM64 and amd64. Video is copied, audio normalised to AAC. One `/data` directory; manual backups.

Fresh installs now include **143 editable US channels**. Bring your own authorised account lists: **no accounts, passwords, or stream URLs are included**. Existing installations keep their channel setup; this update does not automatically overwrite it. Channels become playable as working sources fill their rosters.

Controlled media tests run on native amd64 and ARM64. Broader testing with household Plex clients, non-US channels, and other Docker network setups is still needed.

Open source at [fxPPC/Antennarr](https://github.com/fxPPC/Antennarr). Break it, report it, preferably in that order.

## Run it

Create a dedicated appdata directory, then replace the path and server LAN IP:

```sh
docker run -d --name antennarr --restart unless-stopped \
  -p YOUR_SERVER_LAN_IP:39192:9193 -p YOUR_SERVER_LAN_IP:9192:9192 \
  -e ANTENNARR_ADMIN_LISTEN=:9193 \
  -e ANTENNARR_BASE_URL=http://YOUR_SERVER_LAN_IP:9192 \
  -v /YOUR/APPDATA/antennarr:/data \
  antennarr/antennarr:latest
```

Open `http://YOUR_SERVER_LAN_IP:39192`. Fresh login: **admin / admin**; change it under **Admin → Change login**. Add `http://YOUR_SERVER_LAN_IP:9192` as the tuner in Plex. Keep both ports private to your LAN; use HTTPS through a reverse proxy for public administration. Configure `ANTENNARR_TRUSTED_PROXIES` for that proxy.

**NAS permissions:** default UID/GID is `10001:10001`. To use your NAS account, add `-e PUID=YOUR_UID -e PGID=YOUR_GID`. Startup prepares ownership under `/data`, then runs Antennarr as that non-root user. If your NAS forbids ownership changes, use `--user YOUR_UID:YOUR_GID` instead and give those IDs read/write access to appdata, including existing contents. No privileged mode or world-writable folders needed. SQLite appdata should be local, not SMB/NFS.

**Shared Docker network is optional:** to use one, add `--network YOUR_PLEX_NETWORK`, remove the tuner port mapping, and set `ANTENNARR_BASE_URL=http://antennarr:9192`. Plex must share that existing network; use `http://antennarr:9192` as its tuner. [Docker networking details](https://docs.docker.com/engine/network/drivers/bridge/).

**Upgrading hardened containers:** if you previously used `--cap-drop ALL`, keep the old non-root mode with `--user 10001:10001`, or add `CHOWN`, `DAC_OVERRIDE`, `SETUID`, and `SETGID` for ownership setup. Existing channels and logins are preserved.

Tags: `latest` or pinned `v1.0.2`. Historical beta tags remain available. Beta 2 remains available as `v1.0.0-beta.2` for older data upgrades. Native unRAID playback testing remains pending. Third-party notices: `/usr/share/doc/antennarr/licenses`; [Debian package sources](https://sources.debian.org/).

**Existing installs:** v1/schema-8 data upgrades directly, preserving channels, accounts, settings, login, and tuner identity. Older early-preview/schema-1–7 directories must first run `antennarr/antennarr:v1.0.0-beta.2` to upgrade their data, then v1.0.2. Unsupported databases are refused without replacing their contents. Schema 8 is intended to remain stable through v1. Do not downgrade an upgraded directory. Backups are manual.

Debug-level JSON logs go to Docker by default. Choose None, Normal (warnings/errors), or Debug under Admin. Saved settings take precedence over the bootstrap `ANTENNARR_LOG_LEVEL` environment variable. Enable Docker log rotation.

GPL-3.0-or-later. The image includes buildable source at `/usr/share/doc/antennarr/source.tar.gz`; the source is also available on GitHub. Extract it with `docker cp antennarr:/usr/share/doc/antennarr/source.tar.gz .`.

## New in v1.0.2

- Full accounts release their roster places and return to the random input queue without gaining or resetting strikes. Their accounts and catalogue remain available for later auditions. Antennarr's own live feeds are excluded from external occupancy. The old option to count full connections as failures has been removed, including when loading an older saved configuration.
- Cached UHD rosters cannot restore streams evicted for account capacity. A new successful media audition is required.
- New roster check passes prioritise the oldest verification, with random ties. Input accounts and retry queue positions remain random; checks still run in continuous passes and yield to playback.
- Browser API requests have deadlines, allowing status refreshes to recover from a hanging request. Read requests time out after 15 seconds; mutations allow three minutes and are never retried automatically.
- Docker logs identify unexpected pipeline termination and slow-viewer disconnections without revealing source credentials. Media probes distinguish early endings, process failures, invalid transport, missing timestamp progression, and stalled samples.
- Backup creation releases the administration lock after capturing its input files, before ZIP compression. Snapshot failures and completed staging clean up temporary files. Backups remain manual.

Schema 8 is unchanged. Existing channels, accounts, login, and tuner identity are preserved. Full accounts already saved in rosters are evaluated on their next access check, not removed based solely on old metadata. Controlled tests cover recovery and sharing; they do not establish uninterrupted playback across every provider and Plex client.
