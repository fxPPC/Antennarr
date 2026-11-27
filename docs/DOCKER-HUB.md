# Antennarr

Antennarr turns authorised Xtream account lists into HDHomeRun-compatible tuners for Plex Live TV & DVR. It tests feeds and keeps alternatives for each channel. Plex supplies the guide and records programmes. Bring your own sources.

**v1.0.9 is the final 1.0 release. Future updates will be on the 1.1 release line.**

| Choose | Pull command | Fresh installation |
| --- | --- | --- |
| Stable 1.0 (default) | `docker pull antennarr/antennarr:1.0` | One US tuner with 150 editable channels |
| Latest published preview | `docker pull antennarr/antennarr:testing` | The 1.1 wizard offers optional city tuners |

Both images support Linux amd64 and ARM64. `latest` tracks the recommended stable release, currently `v1.0.9`. To pin the current preview, use `v1.1.0-beta.2`. Stable 1.1.0 is in preparation.

**Upgrading from 1.0?** Download a full backup under **Admin → Backup & restore**. Keep your existing ports, network, and Plex tuner address. The upgrade migrates your data. Returning to 1.0 requires the pre-upgrade backup. Use a separate data directory when trying the preview alongside stable.

[Stable installation](https://github.com/fxPPC/Antennarr/blob/release/1.0/docs/INSTALL.md) · [1.1 preview setup](https://github.com/fxPPC/Antennarr/blob/main/docs/MULTI-TUNER.md) · [Releases and desktop downloads](https://github.com/fxPPC/Antennarr/releases) · [Source and support](https://github.com/fxPPC/Antennarr)
