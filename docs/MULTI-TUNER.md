# Two tuners, one Antennarr

This **v1.1.0-alpha.2 GitHub source preview** adds USA - New York City (150 channels) and UK - London (110 channels). It has not been published to Docker Hub. Stable 1.0 installations remain unchanged.

## Try the preview

Use a separate disposable appdata directory. Do not point two containers at the same directory, or let an alpha open your live 1.0 data. To test an upgrade, restore a **manually created** backup into the disposable directory first.

```sh
git clone --branch v1.1.0-alpha.2 https://github.com/fxPPC/Antennarr.git
cd Antennarr
cp portable.env.example .env
# Edit .env: a separate ANTENNARR_DATA_DIR and an address reachable from Plex.
docker compose -f compose.portable.yaml up -d --build
```

Open administration on port 39192, sign in with `admin/admin` on a fresh install, and change the login. Under Output, both tuner cards are present. Enable the tuners you want, import authorised sources, and wait for suitable streams to fill their rosters. An upgraded primary tuner retains its previous enabled state and channels; the new London tuner starts disabled.

## Private ports

| Purpose | Default container port | Advertised address example |
| --- | --- | --- |
| Existing primary tuner | 9192 | `http://SERVER_LAN_IP:9192` |
| Administration | 9193 (host 39192) | Browser/proxy only |
| First additional tuner, initially London | 9200 | `http://SERVER_LAN_IP:9200` |
| Further tuner slots | 9201–9207 | Same hostname, allocated port |

`ANTENNARR_BASE_URL` supplies the hostname Plex can reach. `ANTENNARR_TUNER_PORTS=9200-9207` reserves eight additional private listeners; a range of 1–64 ports is supported. The supplied Compose files publish that range to `ANTENNARR_BIND`, which defaults to loopback. For Plex on another host, set that bind and the base URL to the Docker server’s LAN address. Keep each additional host port equal to its container port; arbitrary port remapping would make the advertised URL wrong.

A shared user-defined Docker network needs **no published tuner ports**. Use `http://antennarr:9192` as the base URL; London is then `http://antennarr:9200`. Remove the tuner mappings from your local Compose configuration if only network peers need access. Docker documents this distinction in [port publishing](https://docs.docker.com/engine/network/port-publishing/).

No public firewall ports are needed. Send an internet-facing HTTPS administration proxy only to port 9193. The additional listeners expose tuner endpoints, never administration or account APIs. Ports for disabled tuners return 503, and unassigned ports return 404. The old port-5004 compatibility endpoint remains primary-only; it cannot distinguish overlapping channel numbers from two markets.

An added tuner keeps its allocated port and DeviceID across renames and restarts. Deleting a tuner permanently removes that identity; re-adding its preset creates a new identity and the next unused slot, requiring Plex setup again. Removed slots are not recycled. Extend the range’s upper end (keeping its first port unchanged) and matching Compose mappings, then recreate the container if slots are exhausted. Startup rejects a range that overlaps the primary, admin, or compatibility listener.

## Plex and guides

Manually add each enabled tuner address in Plex. Use the US guide appropriate to ZIP **10001** for New York and a UK guide covering London, with a postcode such as **SW1A 1AA**, for London. The London numbers are stable local numbers, not copied satellite guide numbers. Map channels to the correct guide service, especially BBC One London and ITV1 London.

**Real Plex acceptance remains pending.** The alpha has separate HTTP device identities and scoped lineups, but independent guide selection, scheduled recordings with overlapping numbers, and client recovery have not yet been verified in a real multi-device Plex setup. Do not use the preview for essential recordings.

All devices share the configured maximum outgoing streams and account/provider limits. Each advertises that global capacity; the application rejects requests beyond the shared total rather than multiplying capacity by the number of devices. The same channel’s viewers share one primary/hot-backup controller. Different tuner channels have independent IDs and controllers, even when their display names or numbers match.

## Data and future markets

Each profile owns its channels, aliases, exclusions, policies, and rosters. Accounts, source catalogues, and resource budgets remain shared. Output settings such as UHD are global: changing quality mode rebuilds every tuner’s eligible roster, including disabled tuners, with the existing one-hour mode cache. Imports, exports, clear, and reset actions apply only to the selected tuner. Disabling retains saved data, stops background work for that tuner, and blocks new sessions; existing sessions may finish. Removing is a separate confirmed action.

Presets are versioned definitions copied when added. They contain no source accounts, passwords, or playable URLs, and an update does not overwrite your edits. A new market can be added through the preset registry and its own matching tests without creating another streaming engine or database. UK/US lookalikes with uncertain country evidence remain unassigned.

Schema 8 is upgraded to 9 before workers start. Keep a manual pre-upgrade backup to return to 1.0; 1.0 cannot read the expanded format. Full manual backups preserve all tuner identities and channel data, but Docker environment variables, port mappings, and Plex’s own database remain separate.
