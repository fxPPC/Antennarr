Antennarr @VERSION@ for desktop

Docker is the recommended installation. This opt-in 1.1 beta includes FFmpeg,
FFprobe, and their runtime dependencies. No separate tool installation is needed.

Start
  @START@

  Your browser opens when administration is ready (http://127.0.0.1:39192).
  On a fresh installation sign in with admin / admin, change your password in
  Admin, then use the setup wizard to choose a tuner and add authorised sources.

  The Antennarr menu shows viewers and channel coverage. Use Settings to open
  Admin, Start at Login to run quietly after you sign in, and Quit to stop.

Connect Plex
  Fresh installations use port 39192 for local web administration and ports
  39193–39198 for six private LAN tuners. Set ANTENNARR_PORT_RANGE or save
  a range such as 41000-41006 in config/port-range in the data folder, then
  restart. The first port opens the web UI. The next six serve Plex tuners.
  @FIREWALL@
  In Plex Live TV & DVR, use each tuner's address shown in the web UI.
  Existing installations keep their original addresses until reconfigured.

Data and updates
  The launcher saves settings, accounts, rosters, and desktop.log here:
  @DATA@
  To use another writable local directory, set ANTENNARR_DATA before launch.
  Use separate appdata for stable and beta. Before an update, download a full
  backup from Admin and keep it private. Quit the desktop app, replace the old
  application, then reopen it. Disable Start at Login before moving
  the installation to a different path, then enable it again from the new copy.

Troubleshooting
  If startup fails, check desktop.log in the data folder and make sure another
  Antennarr instance is not using the ports. If a media tool is missing, reinstall
  the complete release. To use another compatible pair, set
  ANTENNARR_FFMPEG to the full FFmpeg path with FFprobe in the same directory.
  For foreground automation, run the desktop launcher with ANTENNARR_HEADLESS=1.
  The accompanying antennarr-server executable can also run without the desktop UI.
  On Mac, it is inside Antennarr.app/Contents/Helpers/Antennarr Server.app/Contents/MacOS/.

Source, licences, and build instructions
  https://github.com/fxPPC/Antennarr/tree/@VERSION@
  See LICENSE, licenses/, and the bundled media licences and corresponding source.
