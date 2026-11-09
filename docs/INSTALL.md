# Installation, upgrades, and manual recovery

Use Linux Docker on ARM64 or amd64, with local writable appdata. Debian, FFmpeg, and FFprobe are included in the image. Docker source builds also include the Go toolchain in their build stage; Go is needed on your host only for local development outside Docker. Store SQLite appdata locally, not on SMB/NFS.

## Install the published image

Choose a dedicated appdata directory and your Docker server's LAN address. Replace both placeholders below:

```sh
docker run -d --name antennarr --restart unless-stopped \
  -p YOUR_SERVER_LAN_IP:39192:9193 -p YOUR_SERVER_LAN_IP:9192:9192 \
  -e ANTENNARR_ADMIN_LISTEN=:9193 \
  -e ANTENNARR_BASE_URL=http://YOUR_SERVER_LAN_IP:9192 \
  --log-driver json-file --log-opt max-size=5m --log-opt max-file=2 \
  -v /YOUR/APPDATA/antennarr:/data \
  antennarr/antennarr:v1.0.0
```

Open `http://YOUR_SERVER_LAN_IP:39192`. Initial login is **admin/admin**; change it under **Admin → Change login** before remote exposure. Import authorised account-list URLs under Input and choose channels under Output. Working sources must fill the channel rosters before playback is available.

Add `http://YOUR_SERVER_LAN_IP:9192` manually as the tuner in Plex's Live TV & DVR settings. Select the appropriate guide and map your channels. Antennarr supplies the tuner and media; Plex supplies guide data and recordings. Plex's own Live TV/DVR requirements apply.

## Addresses and Docker networking

| Address | Who uses it | Example |
| --- | --- | --- |
| Administration | Your browser or HTTPS reverse proxy | `http://YOUR_SERVER_LAN_IP:39192` |
| Tuner/base URL | Plex Media Server | `http://YOUR_SERVER_LAN_IP:9192` |

`ANTENNARR_BASE_URL` must be reachable **from Plex**, not just from your browser. A public administration URL is not the tuner address. `127.0.0.1` inside a Plex container refers to that container, not the Docker host.

A shared user-defined Docker network is optional. If Plex shares that network, Antennarr can use `http://antennarr:9192` as its base URL without publishing a tuner port. Ordinary bridge networks do not span separate Docker hosts.

The examples set `ANTENNARR_ADMIN_LISTEN=:9193` to enable the separate administration listener. The main listener defaults to `:9192` and also exposes authenticated administration routes. The separate administration listener omits tuner discovery/playback routes; `/healthz` and `/branding/logo.png` are public on it. Keep the main listener private even when administration is exposed through HTTPS.

## Compose and source builds

Clone the repository, then run from its root:

```sh
cp portable.env.example .env
```

Edit `.env`: set `ANTENNARR_BIND` to your server's LAN address, `ANTENNARR_BASE_URL` to the tuner address Plex can reach, and `ANTENNARR_DATA_DIR` to your dedicated appdata directory. Defaults bind to loopback for local development.

For the published image, set `ANTENNARR_IMAGE=antennarr/antennarr:v1.0.0`, then:

```sh
docker compose -f compose.portable.yaml pull
docker compose -f compose.portable.yaml up -d --no-build
```

For a source build, leave `ANTENNARR_IMAGE=antennarr:local` and use:

```sh
docker compose -f compose.portable.yaml up -d --build
```

`compose.yaml` is an equivalent portable template. To join Plex's existing network, set `PLEX_NETWORK` in `.env` and use both files:

```sh
docker compose -f compose.portable.yaml -f compose.network.yaml up -d --build
```

For a published-image deployment, pull using both files first and replace `--build` with `--no-build`. The overlay sets the base URL to `http://antennarr:9192`; Plex must share that network. It retains the base template's port mappings. You can remove the tuner port mapping in your local Compose configuration if only network peers need it.

## NAS permissions

Default application UID/GID is `10001:10001`. Add `-e PUID=YOUR_UID -e PGID=YOUR_GID` to the run command, or edit those values in `.env`, to use your NAS user/group. The entrypoint prepares ownership under the dedicated `/data` directory, then drops root privileges.

If the NAS forbids ownership changes, use Docker `--user YOUR_UID:YOUR_GID` with an already writable appdata directory. Remove conflicting PUID/PGID values or make them match. Hardened containers using `--cap-drop ALL` need explicit non-root mode, or the ownership/setup capabilities listed in the supplied Compose template. Do not use privileged mode or world-writable appdata to solve a permissions error.

## HTTPS reverse proxy

Forward the public administration hostname to Antennarr's administration listener: container port **9193**, or host port **39192** in these examples. Terminate HTTPS at the proxy. Do not forward the tuner listener publicly.

Set `ANTENNARR_TRUSTED_PROXIES` to the proxy address Antennarr actually sees. For example, if your proxy container has the fixed address `172.30.0.2`, use:

```text
ANTENNARR_TRUSTED_PROXIES=172.30.0.2
```

Replace that example with your actual proxy address. Multiple IPs/CIDRs are comma-separated. Trust only the required proxy peers; do not use `0.0.0.0/0`. Have the proxy set or append the real client address in `X-Forwarded-For`. Configure the proxy and Docker network so direct untrusted clients cannot impersonate a trusted proxy. Recreate the container after environment changes.

## Upgrade

Keep the same `/data` bind mount, tuner URL, and Plex server. Current v1/schema-8 data opens directly, preserving identities and configuration. Old schema-1–7 data must first run `antennarr/antennarr:v1.0.0-beta.2`, then the current version. Unsupported/unversioned populated databases are refused rather than replaced. Do not downgrade an upgraded data directory. Diagnostic columns/settings can be additive within schema 8.

For a published-image Compose installation, change `ANTENNARR_IMAGE` in `.env` to the desired release, stop playback, then run:

```sh
docker compose -f compose.portable.yaml pull
docker compose -f compose.portable.yaml up -d --no-build
```

Include the network overlay in both commands if you use it. A `docker run` installation must pull the chosen image and recreate the container with its original mount, network, ports, and settings; pulling alone does not replace a running container. Backups are manual—create one before an upgrade if you want a recovery copy.

## Manual backup, restore, and channel files

**Admin → Backup & restore** downloads an archive containing accounts and credentials, queue, rosters, check progress, configuration, channel/output assignments, login, and tuner identity. It does not include older backup archives or Plex's database. Keep it private. No backup is made automatically.

Stop viewing before uploading a backup. Restore accepts a ZIP up to **1 GiB**, with at most **2 GiB** of expanded files. It replaces application state and restarts the service. Keep the original tuner URL and Plex server to retain Plex's own Live TV setup. The application backup does not recreate Docker mounts, networking, environment variables, or the reverse proxy; keep your deployment configuration separately.

**Output → Channel lineup export & import** shares definitions without accounts, passwords, source streams, or rosters. Imports replace all channels and rebuild rosters. Use a full manual backup when source state must also be retained.

## Troubleshooting and logs

- **Cannot open administration:** check the bind address, port mapping, and whether the container is running. A loopback bind is reachable only from that host.
- **Plex cannot find the tuner:** verify the base URL from Plex's network environment. Try manual tuner registration; do not use the administration port.
- **Channels will not play:** check roster coverage and recent events. A preset channel definition does not include a working source.
- **Startup reports a permissions error:** verify UID/GID and access to existing appdata contents, not just the top-level folder.
- **Status looks idle:** full rosters pause recruitment; provider pacing or active playback can defer checks. Read the current task and warnings before assuming the checker stopped.

Use `docker logs --tail 200 antennarr` for recent diagnostics. **Admin → Docker logging** applies None, Normal (warnings/errors), or Debug immediately and saves the choice. Saved settings take precedence after startup; the entrypoint can still report a failure before configuration loads. Docker owns log rotation; the supplied templates and run command bound retained logs.

Do not post credentials, full backups, Plex tokens, or source URLs in public issues. Event history is bounded, so aggregate removal totals can outlive individual notices.

Use a persistent container with `--restart unless-stopped`; do not use `--rm` for an installed service. Docker's `system prune` deletes stopped containers independently of Antennarr. Image/builder-cache pruning is a narrower cleanup choice. Never prune appdata volumes.
