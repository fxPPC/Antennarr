# Setting up Plex tuners

Fresh installs start empty. Add any of these presets, review its channels, then enable it. Each tuner has its own lineup and Plex address.

| City | Editable channels | Guide location |
| --- | ---: | --- |
| New York City | 150 | 10001 |
| London | 110 | SW1A 1AA |
| Mexico City | 15 | 06000 |
| Mumbai | 23 | 400001 |
| Berlin | 34 | 10115 |
| Amsterdam | 22 | 1012 JS |

Guide locations are starting points for Plex setup. Check the station and local edition when mapping each channel.

## Try the preview

Use a separate disposable appdata directory. Do not point two containers at the same directory, or let a beta open your live 1.0 data. To test an upgrade, restore a **manually created** backup into the disposable directory first.

```sh
git clone --branch v1.1.0-beta.1 https://github.com/fxPPC/Antennarr.git
cd Antennarr
cp portable.env.example .env
# In .env, choose a separate ANTENNARR_DATA_DIR. For another Plex host, set
# ANTENNARR_BIND and ANTENNARR_PLEX_HOST to this server’s private LAN address.
ANTENNARR_IMAGE=antennarr/antennarr:v1.1.0-beta.1 docker compose -f compose.portable.yaml -f compose.range.yaml pull
ANTENNARR_IMAGE=antennarr/antennarr:v1.1.0-beta.1 docker compose -f compose.portable.yaml -f compose.range.yaml up -d --no-build
```

These commands pull the published image for Linux amd64 or ARM64. If stable and beta run on the same host, give them separate Compose projects, data directories, and non-overlapping listener and host ports (or run the preview on a separate host).

Open the first port in your range (39192 by default), sign in with `admin/admin`, and change the login. The setup wizard opens in **Status** on a fresh installation. Choose a city, enable its tuner, and import an authorised source list. Use the tuner address shown in the last step to add it in Plex. You can skip the wizard and reopen it from **Admin**. Add further cities under **Tuners**. An upgrade preserves saved tuners, their enabled state, and their channels.

## Web and tuner ports

Choose seven consecutive TCP ports with `ANTENNARR_PORT_RANGE`. The first serves web administration. Each of the following six serves one Plex tuner. For example, `41000-41006` puts administration on 41000 and tuners on 41001–41006. Restart Antennarr after changing the range. A change of tuner address also requires updating Plex.

| Fresh-install default | Purpose |
| --- | --- |
| 39192 | Web UI and administration API |
| 39193–39198 | Six tuner slots, with independent device IDs and channel lineups |

The Compose example above uses Docker Compose 2.24.4 or later. It publishes the selected range to loopback by default. To let Plex connect from another computer, set `ANTENNARR_BIND` to the Docker host’s private LAN IP and `ANTENNARR_PLEX_HOST` to the IP or hostname Plex can reach. This makes the web UI reachable on the same private interface. To keep the UI on the host while serving tuners to the LAN, publish the ports separately. For example:

```sh
docker run -d --name antennarr-beta --restart unless-stopped \
  -p 127.0.0.1:41000:41000 \
  -p YOUR_SERVER_LAN_IP:41001-41006:41001-41006 \
  -e ANTENNARR_PORT_RANGE=41000-41006 \
  -e ANTENNARR_BASE_URL=http://YOUR_SERVER_LAN_IP \
  -v /YOUR/BETA/APPDATA:/data antennarr/antennarr:v1.1.0-beta.1
```

For Plex on the same private Docker network, set `ANTENNARR_BASE_URL=http://antennarr` and use `antennarr` as a network alias. The app adds the first tuner port. An HTTPS reverse proxy needs only the administration port. Keep all tuner ports on the private network.

### Upgrading an existing Plex installation

Keep the current Compose file, Docker network name and aliases, port mappings, listener variables, advertised base URL, and appdata. Change the image tag only. Existing installations retain their listener layout, including a primary tuner on 9192, additional tuners beginning at 9200, and the optional primary-only compatibility listener on 5004 when previously configured. Plex can continue using the same addresses and device IDs. Choose the seven-port range explicitly when you are ready to update Plex. The range overlay above is for new or deliberately reconfigured installations.

## Plex and guides

Manually add each enabled tuner address in Plex. Use the US guide appropriate to ZIP **10001** for New York and a UK guide covering London, with a postcode such as **SW1A 1AA**, for London. The London numbers are stable local numbers, not copied satellite guide numbers. Map channels to the correct guide service, especially BBC One London and ITV1 London.

**Real Plex acceptance remains pending.** The beta has separate HTTP device identities and scoped lineups, but independent guide selection, scheduled recordings with overlapping numbers, and client recovery have not yet been verified in a real multi-device Plex setup. Do not use the preview for essential recordings.

All devices share the configured maximum outgoing streams and account/provider limits. Each advertises that global capacity; the application rejects requests beyond the shared total rather than multiplying capacity by the number of devices. The same channel’s viewers share one primary/hot-backup controller. Different tuner channels have independent IDs and controllers, even when their display names or numbers match.

## Data and future markets

Each profile owns its channels, aliases, exclusions, policies, and rosters. Accounts, source catalogues, and resource budgets remain shared. Each tuner has one preferred playback quality: Automatic, 720p, 1080p, or UHD (4K). Choosing or leaving UHD rebuilds that tuner’s rosters, or restores assignments saved within the last hour. The other modes exclude streams labelled UHD. Imports, exports, clear, and reset actions apply only to the selected tuner. Disabling retains saved data, stops background work for that tuner, and blocks new sessions; existing sessions may finish. Removing is a separate confirmed action.

Presets are versioned definitions copied when added. They contain no source accounts, passwords, or playable URLs, and an update does not overwrite your edits. A new market can be added through the preset registry and its own matching tests without creating another streaming engine or database. UK/US lookalikes with uncertain country evidence remain unassigned.

Schema 8 is upgraded to 9 before workers start. Keep a manual pre-upgrade backup to return to 1.0; 1.0 cannot read the expanded format. Full manual backups preserve all tuner identities and channel data, but Docker environment variables, port mappings, and Plex’s own database remain separate.
