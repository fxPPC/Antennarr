# macOS and Windows

Docker on Linux remains the best-supported installation. The standalone apps are part of the opt-in 1.1 beta. Each includes Antennarr, FFmpeg, FFprobe, and the media libraries it needs.

| Computer | Download |
| --- | --- |
| Mac with Apple silicon | [macOS ARM64 disk image](https://github.com/fxPPC/Antennarr/releases/download/v1.1.0-beta.1/antennarr-v1.1.0-beta.1-macos-arm64.dmg) |
| Intel Mac | [macOS Intel disk image](https://github.com/fxPPC/Antennarr/releases/download/v1.1.0-beta.1/antennarr-v1.1.0-beta.1-macos-amd64.dmg) |
| Windows PC with an Intel or AMD processor | [Windows x64 installer](https://github.com/fxPPC/Antennarr/releases/download/v1.1.0-beta.1/antennarr-v1.1.0-beta.1-windows-amd64-setup.exe) |
| Windows on ARM | [Windows ARM64 installer](https://github.com/fxPPC/Antennarr/releases/download/v1.1.0-beta.1/antennarr-v1.1.0-beta.1-windows-arm64-setup.exe) |

## Install and set up

- **Mac:** Open the disk image, drag **Antennarr** onto **Applications**, eject the disk image, and open Antennarr from Applications. Its icon appears in the menu bar.
- **Windows:** Run the installer for your processor. Open **Antennarr Beta** from Start. Its icon appears in the notification area, sometimes under hidden icons.

The beta has no Apple notarisation or Windows publisher signature. The release includes SHA-256 checksums for each installer. [GitHub release and checksums](https://github.com/fxPPC/Antennarr/releases/tag/v1.1.0-beta.1)

On first run, open **Status** in the web interface. Sign in with `admin` / `admin`, change the password under **Admin**, and use the wizard to add a tuner, import an authorised list, and find the address for Plex. The desktop menu shows active viewers and how many enabled channels have a feed. **Settings** opens web administration. **Start at Login** controls automatic launch for your account. **Quit** stops Antennarr and any active streams.

## Plex and the firewall

Fresh installs use seven consecutive TCP ports. By default, the web UI opens at `http://127.0.0.1:39192`, and Plex can reach tuners on ports 39193–39198 on the computer’s private network. The Tuners page shows each Plex address. Set `ANTENNARR_PORT_RANGE` before launch or save a range such as `41000-41006` in the data folder’s `config/port-range` file and restart. The first port serves administration on this computer. The next six serve tuners. An existing installation keeps its old ports until you choose a range.

- **Windows Defender Firewall.** Choose the range in Setup. If Plex runs on another computer, select the private-network firewall option and approve the Windows prompt. Setup allows incoming TCP connections to the six tuner ports from the local subnet on Private networks. Run Setup again when changing ports so the rule matches the new range.
- **Mac firewall.** Allow incoming connections for **Antennarr**. If you turn on the firewall after installing Antennarr, open **System Settings → Network → Firewall → Options**, add `/Applications/Antennarr.app`, and choose **Allow incoming connections**. [Apple’s firewall settings](https://support.apple.com/guide/mac-help/change-firewall-settings-on-mac-mh34041/mac)

## Older systems and upgrades

The app targets macOS 13 or later and 64-bit Windows 10 or later. Individual OS editions and releases have different security-update lifetimes. Use a version that still receives updates. Native release checks cover macOS 15 and 26 on Intel and Apple silicon, Windows 11 on ARM64, and Windows Server on x64. [Apple security releases](https://support.apple.com/en-us/100100) · [Microsoft’s supported Windows versions](https://learn.microsoft.com/en-us/windows/release-health/supported-versions-windows-client)

Data lives in `~/Library/Application Support/Antennarr Beta` on Mac and `%LOCALAPPDATA%\Antennarr Beta` on Windows. Make a manual backup before upgrading. Quit the running app, replace it in Applications or run the new Windows installer, then reopen it. To try existing 1.0 data, restore a backup into a separate beta data directory. Keep the original backup to return to 1.0.
