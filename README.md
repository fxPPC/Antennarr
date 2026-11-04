<img src="docs/images/logo.png" alt="Antennarr mascot" width="140">

# Antennarr

**Stick your antenna into the Internet.** · by **fxPPC**

Antennarr turns authorised IPTV sources into a small, dependable Plex channel lineup. Import URLs containing account lists, choose your channels, and let it build and maintain a roster of checked feeds. When a feed falters, it promotes a live backup and recruits another.

[Docker Hub](https://hub.docker.com/r/antennarr/antennarr) · [Releases](https://github.com/fxPPC/Antennarr/releases) · [Setup help](https://github.com/fxPPC/Antennarr/discussions) · [Report a bug](https://github.com/fxPPC/Antennarr/issues/new/choose)

## What it does

- Presents one stable HDHomeRun-compatible tuner to Plex, with editable channels, numbers, aliases, and exclusion keywords.
- Maintains per-channel rosters, checks sources with provider pacing, and retires failed accounts according to your settings.
- Races startup candidates, keeps a receiving primary and hot backup when available, and shares that pair across viewers watching the same channel.
- Supports simultaneous different channels, configurable viewer limits, source diversity/stability preferences, and optional active-channel replenishment.
- Copies video without transcoding; packages transport output and normalises audio to AAC.
- Offers a mobile-friendly web interface, manual full backup/restore, shareable channel lineups, and configurable Docker logging.

Go, SQLite, and FFmpeg on Debian. One container, one persistent `/data` directory. **No provider accounts, passwords, or playable streams are supplied.** Fresh installations include **143 editable US channels**; you can replace them with your own market's lineup.

## Install with Docker

Create a dedicated appdata directory, then replace the LAN IP and appdata path:

```sh
docker run -d --name antennarr --restart unless-stopped \
  -p YOUR_SERVER_LAN_IP:39192:9193 -p YOUR_SERVER_LAN_IP:9192:9192 \
  -e ANTENNARR_ADMIN_LISTEN=:9193 \
  -e ANTENNARR_BASE_URL=http://YOUR_SERVER_LAN_IP:9192 \
  -v /YOUR/APPDATA/antennarr:/data \
  antennarr/antennarr:latest
```

Open `http://YOUR_SERVER_LAN_IP:39192`. Initial login: **admin / admin**. Change it under **Admin** before remote exposure. Add `http://YOUR_SERVER_LAN_IP:9192` as the tuner in Plex, then select the appropriate local guide and map your channels. Plex handles programme guides; Antennarr does not supply an EPG service.

A shared Docker network is **optional**. If Plex shares an existing user-defined network, you may instead use `--network YOUR_PLEX_NETWORK`, omit the tuner port mapping, and set the base URL/tuner address to `http://antennarr:9192`. Administration still needs its published port.

NAS users can add `-e PUID=YOUR_UID -e PGID=YOUR_GID`. Explicit `--user UID:GID` is also supported when appdata is already writable by that user. Store SQLite appdata locally, not on SMB/NFS. See [installation](docs/INSTALL.md) for Compose, permissions, reverse proxies, and troubleshooting.

Keep tuner endpoints private. Use HTTPS through a correctly configured trusted reverse proxy for remote administration. Backups contain credentials; create and protect them manually.

## Interface

**Input** imports plain or Base64-encoded list URLs. **Output** edits channels and their policies. **Status** shows viewing sessions, roster coverage, checker progress, and recent events. **Admin** contains login, logging, system information, and manual recovery controls.

![Status page with synthetic demonstration data](docs/images/status.png)

Screenshots use disposable synthetic data, not real provider accounts.

## Upgrades and limitations

Read the [v1.0.0 release notes](docs/RELEASE-NOTES.md). Existing **schema-8** data opens directly, retaining settings, channels, accounts, login, and tuner identity. Older schema-1–7 directories must first upgrade through `antennarr/antennarr:v1.0.0-beta.2`. Unsupported data is refused without replacing it. Do not downgrade an upgraded data directory. Backups are manual.

Provider failure, codec changes, broadcast timing differences, and Plex client behaviour can still interrupt viewing. Controlled tests verify source switching and concurrent playback; they do not prove uninterrupted playback on every provider or client. Native unRAID playback testing remains pending. v1 exposes one tuner profile; multiple simultaneous market profiles are future work.

## Build and contribute

A fresh clone builds without private files or access to the maintainer's server:

```sh
git clone https://github.com/fxPPC/Antennarr.git
cd Antennarr
docker compose -f compose.portable.yaml up -d --build
```

This source configuration binds to loopback by default: open `http://127.0.0.1:39192`. Copy `portable.env.example` to `.env` to configure LAN access. All persistent state is under `./data`.

The Docker build uses Go 1.27.3; the module minimum is Go 1.26. For local development, install Go, Node.js 24 for UI tests, and FFmpeg/FFprobe. See [contributing](CONTRIBUTING.md), [tests](docs/TESTING.md), [module boundaries](docs/MODULES.md), [architecture](docs/ARCHITECTURE.md), and [API](docs/API.md). UI contributors can work on the embedded plain JavaScript, HTML, and CSS independently of streaming changes.

Use Discussions for questions and ideas, Issues for reproducible bugs, and [private reporting](https://github.com/fxPPC/Antennarr/security/advisories/new) for vulnerabilities. Never post credentials, account lists, backups, or unredacted logs.

## Licence

Copyright (c) 2026 fxPPC and contributors. **GPL-3.0-or-later**: forks and commercial redistribution are permitted under its terms. No warranty is provided. See [LICENSE](LICENSE) and [third-party notices](docs/licenses/README.md). The image also includes corresponding source at `/usr/share/doc/antennarr/source.tar.gz`.
