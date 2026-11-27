# Setting up Plex tuners

Fresh installations start empty. Each saved tuner has its own lineup and Plex address. Choose from the 20 city presets below. The wizard enables the city you choose. Tuners added later start disabled so you can check their channels before enabling them.

| City | Editable channels | Guide location | Verified Plex lineup |
| --- | ---: | --- | --- |
| New York City | 149 | 10001 | — |
| London | 109 | SW1A 1AA | — |
| Mexico City | 15 | 06000 | — |
| Mumbai | 23 | 400001 | — |
| Berlin | 34 | 10115 | — |
| Amsterdam | 22 | 1012 JS | — |
| Toronto | 28 | M5V 2T6 | Bell Fibe TV |
| Madrid | 28 | 28001 | Movistar TV |
| Paris | 30 | 75001 | Free |
| Rome | 26 | 00184 | Sky Q |
| Warsaw | 30 | 00-001 | Canal+ |
| Lisbon | 29 | 1100 | MEO Fibra |
| São Paulo | 29 | 01001 | Net TV |
| Stockholm | 20 | 11120 | Tele2 |
| Copenhagen | 22 | 1050 | Norlys |
| Oslo | 19 | 0150 | Telenor |
| Buenos Aires | 9 | C1064 | Cablevisión - Capital Federal |
| Bogotá | 13 | 110111 | Claro TV Colombia (DTH) |
| Brussels | 16 | 1000 | Proximus |
| Zürich | 16 | 8001 | Sunrise TV (Deutschschweiz) |

The default port range accommodates six tuners at once.

Guide locations are starting points for Plex setup. Check the station and local edition when mapping each channel.

## First-run setup

[Start the Docker container](INSTALL.md#start-a-fresh-installation) or [install the desktop app](NATIVE.md).

Open the first port in your range (39192 by default), sign in with `admin/admin`, and change the login. The setup wizard opens in **Status** on a fresh installation. Choose a city, enable its tuner, and import an authorised source list. Use the tuner address shown in the last step to add it in Plex. You can skip the wizard and reopen it from **Admin**. Add further cities under **Tuners**. An upgrade preserves saved tuners, their enabled state, and their channels.

Removing a tuner frees its port immediately. The next tuner takes the lowest available slot and receives a new Plex device ID. Disabled tuners keep their slots.

## Web and tuner ports

Choose seven consecutive TCP ports with `ANTENNARR_PORT_RANGE`. The first serves web administration. Each of the following six serves one Plex tuner. For example, `41000-41006` puts administration on 41000 and tuners on 41001–41006. Restart Antennarr after changing the range. A change of tuner address also requires updating Plex.

| Fresh-install default | Purpose |
| --- | --- |
| 39192 | Web UI and administration API |
| 39193–39198 | Six tuner slots, with independent device IDs and channel lineups |

The [Compose installation](INSTALL.md#compose) uses Docker Compose 2.24.4 or later. It publishes the selected range to loopback by default. To let Plex connect from another computer, set `ANTENNARR_BIND` to the Docker host’s private LAN IP and `ANTENNARR_PLEX_HOST` to the IP or hostname Plex can reach. This makes the web UI reachable on the same private interface. To keep the UI on the host while serving tuners to the LAN, publish the ports separately. For example:

```sh
docker run -d --name antennarr --restart unless-stopped \
  -p 127.0.0.1:41000:41000 \
  -p YOUR_SERVER_LAN_IP:41001-41006:41001-41006 \
  -e ANTENNARR_PORT_RANGE=41000-41006 \
  -e ANTENNARR_BASE_URL=http://YOUR_SERVER_LAN_IP \
  -v /YOUR/APPDATA/antennarr:/data antennarr/antennarr:v1.1.0
```

For Plex on the same private Docker network, set `ANTENNARR_BASE_URL=http://antennarr` and use `antennarr` as a network alias. The app adds the first tuner port. An HTTPS reverse proxy needs only the administration port. Keep all tuner ports on the private network.

### Upgrading an existing Plex installation

Download a full backup under **Admin → Backup & restore** and keep a copy of your deployment configuration. Stop playback before switching images. Keep the current Compose file, Docker network name and aliases, port mappings, listener variables, advertised base URL, and appdata. Change the image tag only. Existing installations retain their listener layout, including a primary tuner on 9192, additional tuners beginning at 9200, and the optional primary-only compatibility listener on 5004 when previously configured. Plex can continue using the same addresses and device IDs. Choose the seven-port range explicitly when you are ready to update Plex. Use the range overlay for a new installation or a planned port change.

## Plex and guides

Manually add each enabled tuner address in Plex. Use the US guide appropriate to ZIP **10001** for New York and a UK guide covering London, with a postcode such as **SW1A 1AA**, for London. The London numbers are stable local numbers, not copied satellite guide numbers. Map channels to the correct guide service, especially BBC One London and ITV1 London.

When several tuners share a channel number, check the guide mapping for each device in Plex. [Plex acceptance checks](V1.1-PLAN.md#plex-acceptance)

All devices share the configured maximum outgoing streams and account/provider limits. Each advertises that global capacity; the application rejects requests beyond the shared total rather than multiplying capacity by the number of devices. The same channel’s viewers share one primary/hot-backup controller. Different tuner channels have independent IDs and controllers, even when their display names or numbers match.

## Data and future markets

Each profile owns its channels, aliases, exclusions, policies, and rosters. Accounts, source catalogues, and resource budgets remain shared. Each tuner has one preferred playback quality: Automatic, 720p, 1080p, or UHD (4K). Choosing or leaving UHD rebuilds that tuner’s rosters, or restores assignments saved within the last hour. Automatic, 720p, and 1080p exclude streams labelled UHD, Ultra HD, 4K, 8K, or 2160p/i. Imports, exports, clear, and reset actions apply only to the selected tuner. Disabling retains saved data, stops background work for that tuner, and blocks new sessions; existing sessions may finish. Removing is a separate confirmed action.

Presets are versioned definitions copied when added. They contain no source accounts, passwords, or playable URLs, and an update does not overwrite your edits. A new market can be added through the preset registry and its own matching tests without creating another streaming engine or database. UK/US lookalikes with uncertain country evidence remain unassigned.

Schema 8 is upgraded to 9 before workers start. Keep a manual pre-upgrade backup to return to 1.0; 1.0 cannot read the expanded format. Full manual backups preserve all tuner identities and channel data, but Docker environment variables, port mappings, and Plex’s own database remain separate.
