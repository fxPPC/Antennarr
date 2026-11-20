# Versioning and data compatibility

Antennarr uses [Semantic Versioning 2.0.0](https://semver.org/): compatible fixes increment PATCH, compatible features increment MINOR, and incompatible public-interface changes increment MAJOR. Git and Docker release tags add the conventional `v` prefix. Exact version tags identify a single release. Historical source remains on GitHub. Older Docker images may be removed from Docker Hub.

## Public compatibility contract

The supported interface consists of the documented HTTP API, Plex tuner endpoints and device identities, deployment environment variables, persistent data locations, and portable lineup/backup formats. Additive fields must be tolerated where documented. Internal database tables are not an external API.

Minor upgrades preserve supported existing configuration, accounts, channels, and tuner identities. They may migrate the internal database. On startup, the new version must detect supported older data formats and automatically migrate them, without manual exports, edits, or installation of intermediate versions. A migration must validate its inputs, commit atomically where possible, and never replace an unreadable database with empty data. Keep the same `/data` directory.

For 1.1, all published stable 1.0 data formats must have a tested automatic upgrade path. Run migration before serving requests or starting background writers. Persist its completion only after validation succeeds; interrupted upgrades must roll back or resume safely. Reject unknown/newer formats with a clear error rather than guessing or resetting data. Cross-file changes need a recoverable staged operation because a database transaction alone cannot protect configuration files.

## Data compatibility within stable 1.0 and 1.1

Stable patch releases support in-place downgrades within the same minor version. Upgrades between minor versions preserve existing data; reverting to an earlier minor version requires a pre-upgrade backup.

For example, 1.1.3 → 1.1.0 → 1.1.3 must preserve user configuration and credentials while continuing to work on the same directory. A 1.1 → 1.0 rollback requires restoring the complete matching backup, not just replacing the executable. Stop Antennarr before switching versions or restoring data; two instances must never open the same directory concurrently. A restored backup represents its capture time and cannot retain subsequent edits.

All stable 1.0.x releases share a data format. Stable 1.1.x releases will share their own data format from 1.1.0 onwards. Alpha and beta releases may change format. Test previews on a separate copy of your data. Release candidates should pass the stable compatibility checks before promotion.

Within a stable minor line:

- Preserve database/configuration structures and meanings needed by all earlier stable patches.
- Preserve unfamiliar optional metadata when saving. Test older writers, not just older readers.
- Do not add persistent security or identity rules that earlier patches silently ignore if doing so would make a downgrade unsafe. Such changes need a different release/compatibility decision.
- Test the new version against every previous stable patch in the line using synthetic data. Exercise startup, settings and channel edits, background cleanup, backups/restores, and re-upgrade.
- User-authored settings, identities, channel rules, and credentials must survive. Derived health and roster verification may be refreshed, but refresh must not discard user choices or bypass station rules.

These are release acceptance requirements. They are not implemented or proven merely by adding this document. Stable 1.1.0 must establish the baseline fixture and compatibility harness before release; each later patch must pass the historical-binary matrix.

## Branches and support

- `main` develops the next/current minor line, identified by `RELEASE_LINE`.
- `release/1.0` maintains 1.0 with compatible fixes while 1.1 is developed. Carry relevant fixes forward into `main`; do not merge unfinished features into maintenance branches.
- Feature branches and pull requests target the appropriate line. Both lines run source checks.
- Declare an end-of-support date for 1.0 when stable 1.1 is released. No date has been committed yet. Keep historical downloads after maintenance ends.

## Release tags

| Tag | Meaning |
| --- | --- |
| `v1.0.7` | Immutable exact stable release |
| `1.0` | Most recent published stable 1.0 patch |
| `v1.1.0-alpha.8` | Opt-in ARM64 preview |
| `1.1` | Most recent published stable 1.1 patch; absent until a stable release |
| `latest` | Explicitly promoted recommended stable release |

Prereleases never change stable minor tags, `latest`, or the stable Docker Hub overview. A maintenance release cannot move `latest` behind a newer stable version. Publishing a release never updates a running installation.

## 1.1 public beta format baseline

Use the existing schema 9 catalogue as the beta baseline. An installation may have zero, one, or several saved tuners. Any subset may be enabled. Removing the last tuner is a persistent choice. Disabling it preserves its lineup and roster. A market preset is an optional template for a new tuner. Adding a country to the preset registry must work with the same database format and must leave existing installations' tuner selections, identities, listener slots, lineups, and roster assignments intact.

Channel numbers are local to each tuner. Keep the same internal channel ID when editing a channel. Shared account and provider limits apply across all enabled markets. For scale testing, count enabled channels and their combined roster targets. For example, 500 channels with a target of 15 require 7,500 successful verifications for a full pass. Actual pass duration depends on pacing, response times, playback, and retries.

Before promoting a beta to stable, exercise empty, single-market, and multiple-market installations, a direct 1.0 upgrade, and full backup and restore. Test opening the same data after adding a new market preset to the binary. Treat any proposed format change during beta as an explicit migration with a preservation test and a release note.
