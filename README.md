<img src="docs/images/logo.png" alt="Antennarr mascot" width="140">

# Antennarr

Antennarr turns authorised Xtream account lists into HDHomeRun-compatible tuners for Plex Live TV & DVR. It imports accounts, matches streams to channels, and maintains a tested pool of feeds for each channel. Plex supplies the programme guide and handles recordings. Bring your own sources.

## Choose a release

| Release | Docker Hub tag | Linux platforms | Fresh installation |
| --- | --- | --- | --- |
| Stable 1.0 | `1.0` or `latest`; pin `v1.0.8` for the exact release | amd64, ARM64 | One US tuner with 150 editable channels |
| 1.1 preview | `testing` (newest preview), `v1.1.0-beta.1` (pinned) | amd64, ARM64 | Starts empty. Choose from six optional city presets |

**Docker is the best-supported installation.** The [1.1 beta downloads](https://github.com/fxPPC/Antennarr/releases/tag/v1.1.0-beta.1) also include standalone macOS and Windows apps for ARM64 and amd64, with FFmpeg and FFprobe bundled. [Native installation and firewall settings](docs/NATIVE.md)

**Most users should install stable 1.0.** For the beta, follow [multi-tuner setup](docs/MULTI-TUNER.md). Use a separate data directory. Opening 1.0 data in the beta migrates it to schema 9. Returning to 1.0 requires a pre-upgrade backup.

## Install stable 1.0

Replace the LAN address and appdata path with values for your server. Use local storage for the SQLite database.

```sh
docker run -d --name antennarr --restart unless-stopped \
  -p YOUR_SERVER_LAN_IP:39192:9193 -p YOUR_SERVER_LAN_IP:9192:9192 \
  -e ANTENNARR_ADMIN_LISTEN=:9193 \
  -e ANTENNARR_BASE_URL=http://YOUR_SERVER_LAN_IP:9192 \
  --log-opt max-size=5m --log-opt max-file=2 \
  -v /YOUR/APPDATA/antennarr:/data \
  antennarr/antennarr:1.0
```

1. Open `http://YOUR_SERVER_LAN_IP:39192`. Sign in with `admin/admin` and change the login under **Admin**.
2. Import authorised account-list URLs under **Input** and choose channels under **Output**. A channel becomes playable once it has a working feed.
3. In Plex **Live TV & DVR**, add `http://YOUR_SERVER_LAN_IP:9192`, select the local guide, and map the channels. Keep tuner ports private.

See the [installation guide](docs/INSTALL.md) for Compose, Docker networking, permissions, backups, and troubleshooting.

## How playback works

Antennarr tests candidate feeds and keeps working ones in a channel’s roster. When Plex tunes the channel, Antennarr opens a primary feed and, when available, a live backup. Viewers watching the same channel share those feeds. Antennarr copies video and encodes audio to AAC. Source outages, codec changes, and timing differences can still interrupt playback.

![Feed selection, roster checks, and failover](docs/images/feed-flow.svg)

## Project links

- [Docker Hub tags](https://hub.docker.com/r/antennarr/antennarr/tags) and [GitHub releases](https://github.com/fxPPC/Antennarr/releases)
- [Release notes](docs/RELEASE-NOTES.md), [data compatibility](docs/VERSIONING.md), and [1.1 acceptance tests](docs/V1.1-PLAN.md)
- [Build from source](docs/BUILD.md), [contributing](CONTRIBUTING.md), [architecture](docs/ARCHITECTURE.md), [HTTP API](docs/API.md), and [testing](docs/TESTING.md)
- [Setup questions](https://github.com/fxPPC/Antennarr/discussions), [bug reports](https://github.com/fxPPC/Antennarr/issues/new/choose), and [private security reports](https://github.com/fxPPC/Antennarr/security/advisories/new)

Antennarr is licensed under [GPL-3.0-or-later](LICENSE). Images include [third-party notices](docs/licenses/README.md) and corresponding source at `/usr/share/doc/antennarr/source.tar.gz`.
