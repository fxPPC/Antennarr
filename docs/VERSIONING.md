# Versioning and data compatibility

Antennarr uses [Semantic Versioning 2.0.0](https://semver.org/): compatible fixes increment PATCH, compatible features increment MINOR, and incompatible public-interface changes increment MAJOR. Git and Docker release tags add the conventional `v` prefix. Published version tags and assets are immutable.

## Public compatibility contract

The supported interface consists of the documented HTTP API, Plex tuner endpoints and device identities, deployment environment variables, persistent data locations, and portable lineup/backup formats. Additive fields must be tolerated where documented. Internal database tables are not an external API.

Minor upgrades preserve supported existing configuration, accounts, channels, and tuner identities. They may migrate the internal database. A migration must validate its inputs, commit atomically where possible, and never replace an unreadable database with empty data. Keep the same `/data` directory.

## Patch downgrades, starting with stable 1.1.0

Stable patch releases support in-place downgrades within the same minor version. Upgrades between minor versions preserve existing data; reverting to an earlier minor version requires a pre-upgrade backup.

For example, 1.1.3 → 1.1.0 → 1.1.3 must preserve user configuration and credentials while continuing to work on the same directory. A 1.1 → 1.0 rollback requires restoring the complete matching backup, not just replacing the executable. Stop Antennarr before switching versions or restoring data; two instances must never open the same directory concurrently. A restored backup represents its capture time and cannot retain subsequent edits.

This is an additional Antennarr promise, not a guarantee supplied by SemVer. It starts at stable 1.1.0 and does not retroactively certify 1.0 releases. Alpha, beta, and release-candidate builds have no general downgrade guarantee; use separate disposable copies of appdata until the stable compatibility tests pass.

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
| `v1.0.4` | Immutable exact stable release |
| `1.0` | Most recent published stable 1.0 patch |
| `v1.1.0-alpha.1`, `v1.1.0-beta.1`, `v1.1.0-rc.1` | Immutable, opt-in prereleases |
| `1.1` | Most recent published stable 1.1 patch; absent until a stable release |
| `latest` | Explicitly promoted recommended stable release |

Prereleases never change stable minor tags, `latest`, or the stable Docker Hub overview. A maintenance release cannot move `latest` behind a newer stable version. Publishing a release never updates a running installation.
