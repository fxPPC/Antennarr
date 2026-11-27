# Antennarr

Give each city its own lineup in Plex Live TV & DVR. Antennarr takes your authorised Xtream account lists, matches streams to local channels, and tests primary and backup feeds. Plex supplies the programme guide and handles recordings.

## Run stable 1.1.0

```sh
docker pull antennarr/antennarr:v1.1.0
```

The Linux image supports amd64 and ARM64. Use `latest` for the recommended stable release, `1.1` to follow the 1.1 series, or `v1.1.0` to pin this version. Docker is the best-supported platform.

Start the container, open the web UI, and follow the first-run wizard. Choose from 20 editable city presets and import your authorised sources. The wizard gives you the tuner address and guide location to enter in Plex. The default port range supports six tuners, each with its own lineup. [Docker installation](https://github.com/fxPPC/Antennarr/blob/v1.1.0/docs/INSTALL.md) · [Cities and Plex guides](https://github.com/fxPPC/Antennarr/blob/v1.1.0/docs/MULTI-TUNER.md)

**Upgrading from 1.0?** Download a full backup under **Admin → Backup & restore**. Keep your appdata mount, ports, network, and Plex addresses when replacing the image. Returning to 1.0 requires restoring that backup.

The original US-only series remains available as `antennarr/antennarr:v1.0.9`, with one tuner and 150 editable channels. [Previous release](https://github.com/fxPPC/Antennarr/releases/tag/v1.0.9)

[1.1.0 release and desktop downloads](https://github.com/fxPPC/Antennarr/releases/tag/v1.1.0) · [Source and support](https://github.com/fxPPC/Antennarr)
