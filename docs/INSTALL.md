# Installation, upgrades, and manual recovery

Antennarr 1.1.0 runs on Linux amd64 and ARM64 with Docker, its best-supported platform. The image includes FFmpeg and FFprobe. Use a dedicated writable directory on local storage for SQLite. For macOS and Windows, see the [desktop installers](NATIVE.md).

## Start a fresh installation

Set the host’s private LAN address and the absolute path for appdata. The first port serves web administration. The following six are reserved for Plex tuners. Keep these ports on your private network.

```sh
docker run -d --name antennarr --restart unless-stopped \
  -p YOUR_SERVER_LAN_IP:39192-39198:39192-39198 \
  -e ANTENNARR_PORT_RANGE=39192-39198 \
  -e ANTENNARR_BASE_URL=http://YOUR_SERVER_LAN_IP \
  --log-driver json-file --log-opt max-size=5m --log-opt max-file=2 \
  -v /YOUR/APPDATA/antennarr:/data \
  antennarr/antennarr:v1.1.0
```

Open `http://YOUR_SERVER_LAN_IP:39192`, sign in with `admin/admin`, and change the login under **Admin**. Use the wizard under **Status** to choose a city and import authorised accounts. In Plex **Live TV & DVR**, enter the tuner address from the wizard and select its local guide. Open **Tuners** to add more cities or edit channels. A channel is ready to play once it has a working feed. [Tuner and guide settings](MULTI-TUNER.md)

## Compose

Choose Docker Compose 2.24.4 or later for the range overlay. Clone the release and copy the example environment file.

```sh
git clone --branch v1.1.0 --depth 1 https://github.com/fxPPC/Antennarr.git
cd Antennarr
cp portable.env.example .env
```

In `.env`, set `ANTENNARR_IMAGE=antennarr/antennarr:v1.1.0`, `ANTENNARR_DATA_DIR` to your appdata directory, and `ANTENNARR_BIND` and `ANTENNARR_PLEX_HOST` to the server’s private LAN address. Then start the published image.

```sh
docker compose -f compose.portable.yaml -f compose.range.yaml pull
docker compose -f compose.portable.yaml -f compose.range.yaml up -d --no-build
```

The base template alone provides the legacy listener layout for existing deployments. For an existing private Plex Docker network with a fresh range installation, set `PLEX_NETWORK` to the network name and `ANTENNARR_PLEX_HOST=antennarr`. Include `-f compose.network.yaml` **before** `-f compose.range.yaml` in both commands so the range overlay supplies the advertised address. Plex in a separate container needs a reachable address on that network or the LAN. [Build from source](BUILD.md)

## NAS permissions

Default application UID/GID is `10001:10001`. Add `-e PUID=YOUR_UID -e PGID=YOUR_GID` to the run command, or edit those values in `.env`, to use your NAS user/group. The entrypoint prepares ownership under the dedicated `/data` directory, then drops root privileges.

If the NAS forbids ownership changes, use Docker `--user YOUR_UID:YOUR_GID` with an already writable appdata directory. Remove conflicting PUID/PGID values or make them match. Hardened containers using `--cap-drop ALL` need explicit non-root mode, or the ownership/setup capabilities listed in the supplied Compose template. Do not use privileged mode or world-writable appdata to solve a permissions error.

## HTTPS reverse proxy

Forward the administration hostname to the first port in the range (39192 by default). Terminate HTTPS at the proxy and keep the tuner ports private. On legacy installations, use the existing administration listener.

Set `ANTENNARR_TRUSTED_PROXIES` to the proxy IP address Antennarr sees. The proxy must supply the real client address in `X-Forwarded-For`. Recreate the container after changing the environment.

## Upgrade

1. Download a full backup under **Admin → Backup & restore** and save a copy of the deployment configuration.
2. Stop playback. Change only the image to `antennarr/antennarr:v1.1.0`. Keep the existing data mount, Docker network and aliases, listener variables, port mappings, and advertised Plex address.
3. Pull and recreate the container. With Compose, use `pull` followed by `up -d --no-build` with the same files and project as before. Verify each enabled tuner’s address and lineup in Plex.

The 1.0 upgrade migrates schema 8 to 9 before workers start. The existing primary tuner keeps its identity. Returning to the previous US-only release, v1.0.9, requires restoring your matching 1.0 backup. [Previous installation guide](https://github.com/fxPPC/Antennarr/blob/v1.0.9/docs/INSTALL.md)

## Manual backup, restore, and channel files

**Admin → Backup & restore** downloads an archive containing accounts and credentials, queue, rosters, check progress, configuration, channels and tuner assignments, login, and all tuner identities. It does not include older backup archives or Plex's database. Keep it private. No backup is made automatically. Backup creation captures a consistent SQLite snapshot and copies configuration before releasing the administration lock; ZIP compression then permits status reads and new tunes. Temporary staging needs additional free space on `/data` (up to 2 GiB, plus the database snapshot and archive). Successful manual archives remain in `/data/backups` until you remove them.

Stop viewing before uploading a backup. Restore accepts a ZIP up to **1 GiB**, with at most **2 GiB** of expanded files. It replaces application state and restarts the service. Keep the original tuner URL and Plex server to retain Plex's own Live TV setup. The application backup does not recreate Docker mounts, networking, environment variables, or the reverse proxy; keep your deployment configuration separately.

**Channel lineup export & import** (in the selected tuner) shares definitions without accounts, passwords, source streams, or rosters. A lineup import replaces the selected tuner’s channels and rebuilds its rosters. Other tuners keep their channels. Use a full manual backup when source state must also be retained.

## Troubleshooting and logs

- **Cannot open administration:** check the bind address, port mapping, and whether the container is running. A loopback bind is reachable only from that host.
- **Plex cannot find the tuner:** verify the base URL from Plex's network environment. Try manual tuner registration; do not use the administration port.
- **Channels will not play:** check roster coverage and recent events. A preset channel definition does not include a working source.
- **Startup reports a permissions error:** verify UID/GID and access to existing appdata contents, not just the top-level folder.
- **Status looks idle:** full rosters pause recruitment; provider pacing or active playback can defer checks. Read the current task and warnings before assuming the checker stopped.

Use `docker logs --tail 200 antennarr` for recent diagnostics. **Admin → Docker logging** applies None, Normal (warnings/errors), or Debug immediately and saves the choice. Saved settings take precedence after startup; the entrypoint can still report a failure before configuration loads. Docker owns log rotation; the supplied templates and run command bound retained logs.

Do not post credentials, full backups, Plex tokens, or source URLs in public issues. Event history is bounded, so aggregate removal totals can outlive individual notices.

Use a persistent container with `--restart unless-stopped`; do not use `--rm` for an installed service. Docker's `system prune` deletes stopped containers independently of Antennarr. Image/builder-cache pruning is a narrower cleanup choice. Never prune appdata volumes.
