# Installation, upgrades, and manual recovery

Use Linux Docker on ARM64 or AMD64, with local writable appdata. Debian, FFmpeg, and FFprobe are included in the image; Go is required only for source builds. SQLite appdata must not be on SMB/NFS.

Build with `docker compose up -d --build` or `docker compose -f compose.portable.yaml up -d --build`. Both templates are portable. Copy `portable.env.example` to `.env` and set the server's LAN bind address, the tuner address Plex can reach, and the appdata directory. Fresh login is admin/admin; change it under Admin before remote exposure. NAS users can set PUID/PGID, or use an explicit Docker user with writable appdata.

A shared user-defined Docker network is optional: set PLEX_NETWORK and add `-f compose.network.yaml` to the portable Compose command. Plex must share that network to resolve `antennarr`. Otherwise publish the tuner port on the LAN and set ANTENNARR_BASE_URL accordingly. No VPN or cross-host network is configured. Keep the tuner private; use HTTPS and exact ANTENNARR_TRUSTED_PROXIES for public administration.

## Upgrade

Keep the same /data bind mount, tuner URL, and Plex server. Current v1/schema-8 data opens directly, preserving identities and configuration. Old schema-1–7 data must first run `antennarr/antennarr:v1.0.0-beta.2`, then the current version. Unsupported/unversioned populated databases are refused rather than replaced. Do not downgrade appdata to an older schema. Diagnostic columns/settings are additive within schema 8.

## Manual backup and restore

Admin → Backup & restore downloads a consistent archive containing accounts and credentials, queue, rosters, check progress, configuration, channel/output assignments, login, and tuner identity. Keep it private. No backup is made automatically. Stop viewing before uploading a backup; restore replaces application state and restarts the service. Keep the original tuner URL and Plex server to retain Plex's own Live TV setup; Plex's database is external and is not part of this archive.

## Troubleshooting

Check Docker's JSON logs for the failing task, account ID, failure category, request phase, and HTTP status. Status shows current work and retained events. Do not post credentials, full backups, Plex tokens, or source URLs in public issues. Missing deletion detail is reported explicitly because event history is bounded.

## Logging and container retention

Admin → Docker logging saves the application level and applies it immediately: None, Normal (warnings/errors), or Debug. Saved configuration takes precedence after startup. Docker owns log rotation; the entrypoint may still report a failure before application configuration can load.

Use a persistent container with `--restart unless-stopped`; do not use `--rm` for an installed service. Docker's `system prune` deletes stopped containers, independently of Antennarr. For daily cleanup that preserves stopped services, prune unused images and builder cache rather than all Docker objects. Never prune appdata volumes.

Output → Channel lineup export & import shares definitions without accounts, passwords, source streams, or rosters. Imports replace all channels and rebuild rosters; use a full manual backup when source state must also be retained.
