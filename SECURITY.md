# Security policy

## Report privately

Use [GitHub private vulnerability reporting](https://github.com/fxPPC/Antennarr/security/advisories/new). Do not disclose vulnerability details in public issues or discussions. Include the affected version, reproduction steps using synthetic data, impact, and any suggested fix. Never attach real credentials or backups.

Security fixes target the latest v1 release. There is no guaranteed response time; this is a volunteer project. An automated scan or passing test suite is not an independent security audit.

## Run safely

Fresh login is **admin/admin**; change it before remote exposure. Keep tuner endpoints private. Use HTTPS for remote administration and configure only your actual reverse proxies as trusted. Use only accounts and streams you are authorised to access.

Imported pages are untrusted data: they must not execute code or bypass URL validation. Credentials are stored in restricted files/SQLite under `/data` without an encryption layer. Manual backups include secrets. Keep appdata on local storage with restricted permissions.

Do not post provider credentials, source URLs, Plex tokens, SSH keys, full backups, private server details, or unredacted diagnostic archives. Antennarr does not need a Docker socket or privileged mode.
