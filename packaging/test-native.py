"""Install a release DMG, Windows Setup, or portable ZIP, then test its native app."""

import argparse
import base64
import contextlib
import ipaddress
import json
import mmap
import os
from pathlib import Path
import platform
import random
import re
import shutil
import signal
import socket
import sqlite3
import struct
import subprocess
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlparse
from urllib.request import build_opener, ProxyHandler, Request
import zipfile


MAC_SERVER = Path("Contents/Helpers/Antennarr Server.app")
HTTP = build_opener(ProxyHandler({}))
AUTH = "Basic " + base64.b64encode(b"admin:admin").decode()


def api(base, route, body=None):
    data = json.dumps(body).encode() if body is not None else None
    request = Request(base + route, data=data, headers={
        "Authorization": AUTH, "Content-Type": "application/json", "X-Antennarr-Request": "1",
    })
    try:
        with HTTP.open(request, timeout=5) as response:
            return json.load(response)
    except HTTPError as exc:
        raise RuntimeError(f"{route}: HTTP {exc.code}: {exc.read(512).decode(errors='replace')}") from exc


def free_port_range():
    # Keep the test listeners out of the OS pool used for outgoing connections.
    for _ in range(100):
        with contextlib.ExitStack() as stack:
            base = random.randrange(20000, 39000 - 6)
            try:
                for port in range(base, base + 7):
                    listener = stack.enter_context(socket.socket())
                    listener.bind(("", port))
                    listener.listen()
            except OSError:
                continue
            return base
    raise RuntimeError("Cannot find seven free consecutive TCP ports")


def stop(process):
    if process.poll() is not None:
        return
    if os.name == "nt":
        # Windows GUI controllers and their console servers can have no shared console.
        subprocess.run(["taskkill", "/pid", str(process.pid), "/t", "/f"],
                       stdout=subprocess.DEVNULL, check=False, timeout=15)
    else:
        # Give the desktop controller a chance to shut down its own server.
        process.send_signal(signal.SIGTERM)
    try:
        process.wait(timeout=15)
    except subprocess.TimeoutExpired:
        if os.name == "nt":
            process.kill()
        else:
            os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=5)


@contextlib.contextmanager
def running(executable, env, base, logfile):
    with logfile.open("wb") as output:
        finder = platform.system() == "Darwin" and firewall_requested()
        command = [executable]
        if finder:
            # Launch Services starts the normal menu app. Its login argument suppresses the browser.
            command = ["/usr/bin/open", "-n", "-W", "-g", "-a", executable.parents[2],
                       "--stdout", logfile, "--stderr", logfile]
            for key, value in env.items():
                if key == "PATH" or (key.startswith("ANTENNARR_") and key != "ANTENNARR_HEADLESS"):
                    command.extend(["--env", key + "=" + value])
            command.extend(["--env", "ANTENNARR_HEADLESS=0", "--args", "--login-item"])
        process = subprocess.Popen(command, cwd=executable.parent, env=env,
                                   stdout=output, stderr=subprocess.STDOUT,
                                   start_new_session=os.name != "nt")
        try:
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    raise RuntimeError(f"Launcher exited with status {process.returncode}")
                try:
                    if api(base, "/healthz") == {"status": "ok", "stage": "ready"}:
                        break
                except (URLError, TimeoutError, ConnectionError):
                    pass
                time.sleep(0.15)
            else:
                raise RuntimeError("Timed out waiting for the native server")
            yield
        except Exception:
            print(logfile.read_text(errors="replace")[-4000:])
            raise
        finally:
            if finder:
                # open -W waits for the app but signals sent to open do not reach it.
                listing = subprocess.check_output(["/bin/ps", "-axww", "-o", "pid=,comm="], text=True)
                for line in listing.splitlines():
                    entry = line.strip().split(None, 1)
                    if len(entry) == 2 and entry[1] == str(executable):
                        os.kill(int(entry[0]), signal.SIGTERM)
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    pass
            stop(process)


def verify_dependency_error(launcher, env, root):
    # An explicit bad path must produce a usable diagnostic without opening a window.
    bad_env = {**env, "ANTENNARR_FFMPEG": str(root / "not-installed" / "ffmpeg")}
    with tempfile.TemporaryFile() as output:
        process = subprocess.Popen([launcher], cwd=launcher.parent, env=bad_env,
                                   stdout=output, stderr=subprocess.STDOUT,
                                   start_new_session=os.name != "nt")
        try:
            result = process.wait(timeout=15)
        finally:
            stop(process)
        output.seek(0)
        message = output.read().decode(errors="replace")
    assert result != 0 and "ffmpeg" in message.lower(), f"Missing dependency diagnostic: {message!r}"


def desktop_menu(launcher, env, admin_base, root):
    # Exercise native menu handlers with the platform's isolated test login entry.
    menu_env = {**env, "ANTENNARR_MENU_TEST": "1"}
    with (root / "menu.log").open("w+b") as log:
        process = subprocess.Popen([launcher], cwd=launcher.parent, env=menu_env,
                                   stdout=log, stderr=subprocess.STDOUT,
                                   start_new_session=os.name != "nt")
        try:
            result = process.wait(timeout=45)
        finally:
            stop(process)
        log.seek(0)
        output = log.read().decode(errors="replace")
    assert result == 0, output[-4000:]
    reports = []
    for line in output.splitlines():
        if line.startswith("{"):
            row = json.loads(line)
            if "settings_url" in row:
                reports.append(row)
    assert len(reports) == 1, output[-4000:]
    assert reports[0] == {"viewers": 0, "channels": 0, "settings_url": admin_base + "/admin#admin",
                          "login_enabled": True, "login_disabled": True, "quit": True}, reports[0]
    try:
        api(admin_base, "/healthz")
    except (URLError, ConnectionError):
        return
    raise AssertionError("Quit left the server running")


def fixture_stream(base, tuner, ffmpeg, ffprobe, root, env):
    fixture = root / "mpeg2.ts"
    subprocess.run([
        ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i",
        "testsrc2=size=320x240:rate=25", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
        "-t", "2", "-c:v", "mpeg2video", "-g", "12", "-b:v", "800k", "-c:a", "mp2",
        "-f", "mpegts", str(fixture),
    ], check=True, timeout=30, env=env)
    lineup = api(tuner, "/lineup.json")
    entry = next(ch for ch in lineup if ch["GuideNumber"] == "9999")
    assert entry["GuideName"] == "Native fixture"
    assert entry["URL"] == tuner + "/auto/v9999"
    with HTTP.open(entry["URL"], timeout=35) as response:
        sample = response.read(188 * 400)
    api(base, "/api/outgoing/mpeg2/stop", {})
    assert len(sample) == 188 * 400
    probe = subprocess.run([ffprobe, "-v", "error", "-show_entries", "stream=codec_name",
                            "-of", "json", "-i", "pipe:0"], input=sample, stdout=subprocess.PIPE,
                           check=True, timeout=20, env=env)
    assert {s["codec_name"] for s in json.loads(probe.stdout)["streams"]} == {"mpeg2video", "mp2"}


def windows_imports(path, arch):
    # PE32+ import directory: each 20-byte descriptor contains the DLL name RVA.
    # Translate relative virtual addresses through the section table to file offsets.
    with path.open("rb") as src, mmap.mmap(src.fileno(), 0, access=mmap.ACCESS_READ) as data:
        (pe,) = struct.unpack_from("<I", data, 0x3c)
        assert data[pe:pe + 4] == b"PE\x00\x00", f"{path.name}: invalid PE signature"
        machine, count = struct.unpack_from("<HH", data, pe + 4)
        assert machine == {"amd64": 0x8664, "arm64": 0xaa64}[arch], f"{path.name}: wrong architecture"
        (header_size,) = struct.unpack_from("<H", data, pe + 20)
        optional = pe + 24
        assert struct.unpack_from("<H", data, optional)[0] == 0x20b, f"{path.name}: expected PE32+"
        (directory,) = struct.unpack_from("<I", data, optional + 120)
        if not directory:
            return []
        sections = [struct.unpack_from("<IIII", data, optional + header_size + n * 40 + 8)
                    for n in range(count)]

        def offset(rva):
            for virtual_size, address, size, raw in sections:
                if address <= rva < address + max(virtual_size, size):
                    return raw + rva - address
            raise ValueError(f"{path.name}: invalid import RVA {rva}")

        pos = offset(directory)
        result = []
        while any(data[pos:pos + 20]):
            (name_rva,) = struct.unpack_from("<I", data, pos + 12)
            name = offset(name_rva)
            end = data.find(b"\0", name)
            assert end > name, f"{path.name}: invalid import name"
            dll = data[name:end].decode("ascii").lower()
            if not re.fullmatch(r"[a-z0-9][a-z0-9_.-]*\.dll", dll):
                raise ValueError(f"{path.name}: invalid DLL import name {dll!r}")
            result.append(dll)
            pos += 20
        return result


def windows_system_directory():
    import ctypes
    directory = ctypes.create_unicode_buffer(32768)
    size = ctypes.windll.kernel32.GetSystemDirectoryW(directory, len(directory))
    if not 0 < size < len(directory):
        raise ctypes.WinError()
    return Path(directory.value)


def check_native_files(package, executables, arch):
    if os.name == "nt":
        system = windows_system_directory()
        for binary in [*executables, *package.rglob("*.dll")]:
            for name in windows_imports(binary, arch):
                if name.startswith(("api-ms-", "ext-ms-")) or (binary.parent / name).is_file():
                    continue
                # The standalone download also needs its own non-inbox Visual C++ runtime.
                assert (system / name).is_file() and not name.startswith(("vcruntime", "msvcp1")), (
                    f"{binary.name}: missing bundled dependency {name}")
        return
    machine = {"amd64": "x86_64", "arm64": "arm64"}[arch]
    for binary in [*executables, *package.rglob("*.dylib")]:
        slices = subprocess.check_output(["lipo", "-archs", binary], text=True).split()
        assert machine in slices, f"{binary.name}: expected {machine}, got {slices}"
        loads = subprocess.check_output(["otool", "-arch", machine, "-L", binary], text=True)
        for line in loads.splitlines()[1:]:
            dep = line.strip().split(" (compatibility version", 1)[0]
            if dep.startswith(("/usr/lib/", "/System/Library/")):
                continue
            if dep.startswith(("@executable_path/", "@loader_path/")):
                local = (binary.parent / dep.split("/", 1)[1]).resolve()
                assert local.is_relative_to(package) and local.is_file(), f"{binary.name}: {dep}"
            else:
                assert dep.startswith("@rpath/") and any(package.rglob(Path(dep).name)), (
                    f"{binary.name}: external dependency {dep}")


def firewall_state():
    script = """
        $ErrorActionPreference = 'Stop'
        $p = New-Object -ComObject HNetCfg.FwPolicy2
        $rule = $null
        try {
            $rule = $p.Rules.Item('Antennarr Beta LAN tuner') |
                Select-Object ApplicationName,Enabled,Direction,Action,Profiles,Protocol,LocalPorts,RemoteAddresses
        } catch {
            # INetFwRules::Item returns HRESULT_FROM_WIN32(ERROR_FILE_NOT_FOUND) for an absent rule.
            if ($_.Exception.GetBaseException().HResult -ne -2147024894) { throw }
        }
        @{privateEnabled=$p.FirewallEnabled(2); activeProfiles=$p.CurrentProfileTypes; rule=$rule} | ConvertTo-Json -Compress -Depth 3
    """
    out = subprocess.check_output(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script],
                                  text=True, timeout=60)
    return json.loads(out)


def firewall_requested():
    requested = os.environ.get("ANTENNARR_TEST_FIREWALL") == "1"
    if requested and os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted":
        raise RuntimeError("Firewall changes are restricted to disposable GitHub-hosted runners.")
    return requested


@contextlib.contextmanager
def mac_firewall(app):
    tool = "/usr/libexec/ApplicationFirewall/socketfilterfw"
    def read(*args):
        return subprocess.check_output([tool, *args], text=True, timeout=15).strip()
    def configure(*args):
        subprocess.run(["sudo", "-n", tool, *args], check=True, timeout=20)
    state = read("--getglobalstate")
    print("macOS application firewall:", state)
    if not firewall_requested():
        yield
        return
    app = app.resolve()
    # These are the two apps a user can allow in Firewall Options.
    targets = [str(app), str(app / MAC_SERVER)]
    assert not any(target in read("--listapps") for target in targets)
    restore_off = "disabled" in state.lower()
    block_all = read("--getblockall")
    restore_block_all = "enabled" in block_all.lower()
    added = []
    try:
        configure("--setglobalstate", "on")
        assert "enabled" in read("--getglobalstate").lower()
        # Block-all overrides individual application rules.
        if restore_block_all:
            configure("--setblockall", "off")
        assert "disabled" in read("--getblockall").lower()
        for target in targets:
            configure("--add", target)
            added.append(target)
            configure("--unblockapp", target)
            allowed = read("--getappblocked", target)
            assert re.search(r"permit|allow|not blocked|unblocked", allowed, re.I), allowed
            print(f"macOS firewall rule for {target}: {allowed}", flush=True)
        yield
    finally:
        try:
            for target in reversed(added):
                if target in read("--listapps"):
                    configure("--remove", target)
            assert not any(target in read("--listapps") for target in added)
        finally:
            if restore_block_all:
                configure("--setblockall", "on")
            if restore_off:
                configure("--setglobalstate", "off")


def wait_for_mac_tuner(base):
    # ALF reloads its policy asynchronously when the CI runner enables the
    # firewall. Require an actual tuner response before testing its routes.
    deadline = time.monotonic() + 25
    while True:
        try:
            return api(base, "/discover.json")
        except (URLError, TimeoutError, ConnectionError) as exc:
            if time.monotonic() >= deadline:
                port = urlparse(base).port
                listeners = subprocess.run(["/usr/sbin/lsof", "-nP", f"-iTCP:{port}", "-sTCP:LISTEN"],
                                           capture_output=True, text=True, timeout=5)
                # Distinguish a client-specific failure from a system-wide
                # refusal, and retain the firewall decision before the VM exits.
                probe = subprocess.run(["/usr/bin/curl", "--noproxy", "*", "--silent", "--show-error",
                                        "--connect-timeout", "2", "--max-time", "4", "--output", "/dev/null",
                                        "--write-out", "%{http_code}", base + "/discover.json"],
                                       capture_output=True, text=True, timeout=6)
                print("macOS system HTTP client:", probe.returncode, probe.stdout, probe.stderr.strip())
                predicate = ('(process == "socketfilterfw" OR process == "Firewall" OR '
                             'subsystem BEGINSWITH "com.apple.alf" OR subsystem == "com.apple.networkextension") '
                             'AND (eventMessage CONTAINS[c] "antennarr" OR eventMessage CONTAINS[c] "drop" '
                             'OR eventMessage CONTAINS[c] "deny")')
                decisions = subprocess.run(["/usr/bin/log", "show", "--last", "90s", "--style", "compact",
                                            "--info", "--predicate", predicate], capture_output=True, text=True, timeout=20)
                print("macOS firewall events:", (decisions.stdout or decisions.stderr)[-4000:], flush=True)
                raise RuntimeError(f"macOS firewall test could not reach {base}; "
                                   f"local listeners: {listeners.stdout.strip()}") from exc
            time.sleep(0.4)


def set_private_firewall(enabled):
    subprocess.run(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
                    "Set-NetFirewallProfile -Profile Private -Enabled " + ("True" if enabled else "False")],
                   check=True, timeout=30)


def verify_windows_rule(server, admin):
    state = firewall_state()
    assert state["privateEnabled"]
    rule = state["rule"]
    assert rule, "Setup completed with lanfirewall selected, but its named rule is missing"
    assert Path(os.path.expandvars(rule["ApplicationName"])).samefile(server), rule
    assert rule["Enabled"] and rule["Profiles"] == 2 and rule["Direction"] == 1 and rule["Action"] == 1
    assert rule["Protocol"] == 6 and rule["LocalPorts"] == f"{admin + 1}-{admin + 6}", rule
    scopes = {x.casefold() for x in rule["RemoteAddresses"].split(",")}
    assert scopes and scopes <= {"localsubnet", "localsubnet4", "localsubnet6"}, scopes


@contextlib.contextmanager
def new_mac_bundle(folder):
    app = folder / "Antennarr.app"
    app.mkdir()  # Refuse an existing application or symlink.
    created = app.stat()
    try:
        yield app
    finally:
        if app.exists() or app.is_symlink():
            if not os.path.samestat(created, app.lstat()):
                raise RuntimeError(f"The test bundle at {app} was replaced; leaving it in place")
            shutil.rmtree(app)


@contextlib.contextmanager
def installed(archive, root):
    package = root / "Test Applications"
    if archive.suffix == ".zip":
        package.mkdir()
        with zipfile.ZipFile(archive) as z:
            for info in z.infolist():
                extracted = Path(z.extract(info, package))
                # Python drops ZIP permission bits. Use its normalised extraction path.
                if platform.system() == "Darwin" and not info.is_dir() and info.external_attr >> 16 & 0o111:
                    extracted.chmod(0o755)
        yield package
    elif archive.suffix == ".dmg":
        hosted = firewall_requested()
        if hosted:
            package = Path("/Applications")
        else:
            package.mkdir()
        with new_mac_bundle(package) as app:
            mount = root / "Mounted image"
            mount.mkdir()
            subprocess.run(["hdiutil", "verify", "-quiet", archive], check=True, timeout=100)
            subprocess.run(["hdiutil", "attach", "-quiet", "-readonly", "-nobrowse", "-noautoopen",
                            "-mountpoint", mount, archive], check=True, timeout=90)
            try:
                assert os.statvfs(mount).f_flag & os.ST_RDONLY
                assert (mount / "Applications").is_symlink() and os.readlink(mount / "Applications") == "/Applications"
                assert (mount / "Read Me.txt").is_file()
                subprocess.run(["codesign", "--verify", "--deep", "--strict", mount / "Antennarr.app"], check=True)
                subprocess.run(["ditto", mount / "Antennarr.app", app], check=True, timeout=90)
            finally:
                subprocess.run(["hdiutil", "detach", "-quiet", mount], check=True, timeout=60)
            with contextlib.ExitStack() as cleanup:
                if hosted:
                    # Finder registers an app when it is dragged to Applications.
                    registrar = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
                    for registered in (app / MAC_SERVER, app):
                        subprocess.run([registrar, "-f", registered], check=True, timeout=30)
                        cleanup.callback(subprocess.run, [registrar, "-u", registered], check=True, timeout=30)
                with mac_firewall(app):
                    yield package
    else:
        import ctypes
        import winreg
        key = r"Software\Microsoft\Windows\CurrentVersion\Uninstall\Antennarr.Beta_is1"
        try:
            previous = winreg.OpenKey(winreg.HKEY_CURRENT_USER, key, 0, winreg.KEY_READ | winreg.KEY_WOW64_64KEY)
        except FileNotFoundError:
            pass
        else:
            previous.Close()
            raise RuntimeError("Test the installer on a clean Windows user profile; an Antennarr Beta installation exists.")
        package = package.resolve()
        flags = ["/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART"]
        data = root / "Application Data"
        range_path = data / "config/port-range"
        install_env = {k: v for k, v in os.environ.items() if not k.upper().startswith("ANTENNARR_")}
        install_env["ANTENNARR_DATA"] = str(data)
        server = package / "antennarr-server.exe"
        state = firewall_state()
        print("Windows private firewall enabled:", state["privateEnabled"], "active profile mask:", state["activeProfiles"])
        assert state["rule"] is None, f"An existing Antennarr firewall rule is present: {state['rule']}"
        uninstaller = package / "unins000.exe"
        active_firewall_test = firewall_requested()
        if active_firewall_test and not ctypes.windll.shell32.IsUserAnAdmin():
            raise RuntimeError("Testing the private firewall rule requires an elevated disposable runner.")
        try:
            invalid = subprocess.run([archive, *flags, "/PORTRANGE=40000-40007", "/TASKS=", "/DIR=" + str(package)],
                                     timeout=60, env=install_env)
            assert invalid.returncode != 0 and not uninstaller.exists() and not range_path.exists()
            subprocess.run([archive, *flags, "/LOG=" + str(root / "install.log"), "/TASKS=", "/DIR=" + str(package)],
                           check=True, timeout=120, env=install_env)
            assert uninstaller.is_file() and firewall_state()["rule"] is None
            assert range_path.read_text().strip() == "39192-39198"
            if active_firewall_test:
                if not state["privateEnabled"]:
                    set_private_firewall(True)
                subprocess.run([archive, *flags, "/LOG=" + str(root / "lanfirewall.log"), "/TASKS=lanfirewall", "/DIR=" + str(package)],
                               check=True, timeout=120, env=install_env)
                verify_windows_rule(server, 39192)
            base = free_port_range()
            selected = f"{base}-{base + 6}"
            subprocess.run([archive, *flags, "/LOG=" + str(root / "range.log"), "/PORTRANGE=" + selected,
                            "/DIR=" + str(package)], check=True, timeout=120, env=install_env)
            assert range_path.read_text().strip() == selected
            if active_firewall_test:
                verify_windows_rule(server, base)
            yield package
        finally:
            try:
                if uninstaller.exists():
                    subprocess.run([uninstaller, *flags, "/LOG=" + str(root / "uninstall.log")],
                                   check=True, timeout=100)
                    # Inno finishes deleting its own executable after the first process exits.
                    deadline = time.monotonic() + 30
                    while time.monotonic() < deadline and (server.exists() or uninstaller.exists()):
                        time.sleep(0.25)
                    remaining = [str(p) for p in (server, uninstaller) if p.exists()]
                    assert not remaining, f"Uninstall left files behind: {remaining}"
                rule = firewall_state()["rule"] if active_firewall_test else None
                if rule:
                    assert Path(os.path.expandvars(rule["ApplicationName"])).resolve() == server, rule
                    subprocess.run(["netsh.exe", "advfirewall", "firewall", "delete", "rule",
                                    "name=Antennarr Beta LAN tuner", "program=" + str(server), "dir=in"],
                                   check=True, timeout=20)
                    raise AssertionError("Uninstall left its test firewall rule behind")
            finally:
                if active_firewall_test and not state["privateEnabled"]:
                    set_private_firewall(False)
                for log in (root / "install.log", root / "lanfirewall.log", root / "range.log", root / "uninstall.log"):
                    if log.exists():
                        raw = log.read_bytes()
                        encoding = "utf-16" if raw.startswith(b"\xff\xfe") else "utf-8-sig"
                        for line in raw.decode(encoding, errors="replace").splitlines():
                            if "Antennarr firewall" in line or "error" in line.lower():
                                print(log.name, line)
        config = root / "Application Data" / "config" / "config.json"
        assert json.loads(config.read_text())["settings"]["scans_paused"] is True
        database = root / "Application Data" / "database" / "antennarr.db"
        with contextlib.closing(sqlite3.connect(database.as_uri() + "?mode=ro", uri=True)) as db:
            assert db.execute("SELECT name FROM channels WHERE id='mpeg2'").fetchone() == ("Native fixture",)


def tuner_only(base):
    for route in ["/api/status", "/api/login"]:
        try:
            with HTTP.open(Request(base + route, headers={"Authorization": AUTH}), timeout=4):
                raise AssertionError(f"Authenticated administration is exposed on tuner route {base}{route}")
        except HTTPError as exc:
            assert exc.code == 404, f"{route}: expected tuner-only 404, got {exc.code}"


def smoke(archive, root):
    os_name = {"Darwin": "macos", "Windows": "windows"}[platform.system()]
    arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[platform.machine().lower()]
    ext = r"(?:\.zip|\.dmg)" if os_name == "macos" else r"(?:\.zip|-setup\.exe)"
    match = re.fullmatch(r"antennarr-v1\.1\.[0-9]+-beta\.[0-9]+-" + os_name + "-" + arch + ext, archive.name)
    assert match, f"Expected a release for {os_name}-{arch}, received {archive.name}"
    with installed(archive, root) as package:
        exercise(package, root, os_name, arch)
    print(f"{archive.name}: install, dependency diagnostic, audio/video, restart, network separation, and menu passed")


def exercise(package, root, os_name, arch):
    if os_name == "macos":
        app = package / "Antennarr.app"
        bindir = app / "Contents/MacOS"
        launcher = bindir / "Antennarr"
        server = app / MAC_SERVER / "Contents/MacOS/antennarr-server"
        media = bindir.parent / "Resources" / "media" / "bin"
        ffmpeg, ffprobe = media / "ffmpeg", media / "ffprobe"
    else:
        launcher, server = package / "Antennarr.exe", package / "antennarr-server.exe"
        media = package / "media" / "bin"
        ffmpeg, ffprobe = media / "ffmpeg.exe", media / "ffprobe.exe"
    for path in [launcher, server, ffmpeg, ffprobe]:
        assert path.is_file() and os.access(path, os.X_OK), f"Missing executable {path}"

    check_native_files(package / "Antennarr.app" if os_name == "macos" else package, [launcher, server, ffmpeg, ffprobe], arch)
    env = {k: v for k, v in os.environ.items() if not k.upper().startswith("ANTENNARR_")}
    data = root / "Application Data"
    saved_range = data / "config/port-range"
    if saved_range.exists():
        admin = int(saved_range.read_text().split("-", 1)[0])
    elif firewall_requested():
        admin = 39192
    else:
        admin = free_port_range()
        env["ANTENNARR_PORT_RANGE"] = f"{admin}-{admin + 6}"
    admin_base = f"http://127.0.0.1:{admin}"
    if os.name == "nt":
        system = windows_system_directory()
        env["PATH"] = os.pathsep.join(map(str, [system, system.parent]))
    else:
        env["PATH"] = "/usr/bin:/bin"
        env = {k: v for k, v in env.items() if not k.startswith("DYLD_")}
    for name in ["ffmpeg", "ffprobe"]:
        assert not shutil.which(name, path=env["PATH"]), f"{name} found on isolated test PATH"
    for tool in [ffmpeg, ffprobe]:
        subprocess.run([tool, "-version"], stdout=subprocess.DEVNULL, check=True, timeout=10, env=env)
    env.update(ANTENNARR_HEADLESS="1", ANTENNARR_DATA=str(data), ANTENNARR_FIXTURES=str(root))
    verify_dependency_error(launcher, env, root)
    with running(launcher, env, admin_base, root / "first-run.log"):
        try:
            HTTP.open(admin_base + "/admin", timeout=3)
        except HTTPError as exc:
            assert exc.code == 401
        else:
            raise AssertionError("Administration should require a login")
        request = Request(admin_base + "/admin", headers={"Authorization": AUTH})
        with HTTP.open(request, timeout=5) as response:
            assert b"Antennarr" in response.read()
        assert api(admin_base, "/api/settings", {"scans_paused": True})["settings"]["scans_paused"]
        channel = {"id": "mpeg2", "number": "9999", "name": "Native fixture", "enabled": True}
        assert api(admin_base, "/api/profiles") == []
        for preset in ("in-mumbai", "de-berlin", "nl-amsterdam", "mx-mexico-city", "us-nyc", "uk-london"):
            profile = api(admin_base, "/api/profiles", {"preset_id": preset})
            api(admin_base, "/api/profiles/" + profile["id"], {"enabled": True})
        profiles = api(admin_base, "/api/profiles")
        saved = profiles[0]
        tuner_host = urlparse(saved["tuner_url"]).hostname
        address = ipaddress.IPv4Address(tuner_host)
        assert address.is_private and (not firewall_requested() or not address.is_loopback), address
        with socket.socket() as local:
            local.bind((tuner_host, 0))
        expected = {f"http://{tuner_host}:{port}" for port in range(admin + 1, admin + 7)}
        assert {p["tuner_url"] for p in profiles} == expected
        for p in profiles:
            url = p["tuner_url"]
            if os_name == "macos" and firewall_requested():
                discovery = wait_for_mac_tuner(url)
            else:
                discovery = api(url, "/discover.json")
            assert discovery["FriendlyName"] == p["name"]
            tuner_only(url)
            assert len(api(url, "/lineup.json")) == p["channel_count"]
        channel["profile_id"], tuner = saved["id"], saved["tuner_url"]
        assert urlparse(tuner).hostname == tuner_host
        assert api(admin_base, "/api/channels", channel)["id"] == "mpeg2"
        assert isinstance(api(admin_base, "/api/status"), dict)
        if firewall_requested():
            try:
                with socket.create_connection((tuner_host, admin), timeout=3):
                    raise AssertionError("The admin listener is reachable on the LAN interface")
            except (ConnectionRefusedError, TimeoutError):
                pass
        device = api(tuner, "/discover.json")["DeviceID"]
        assert re.fullmatch(r"[0-9A-F]{8}", device)
        fixture_stream(admin_base, tuner, ffmpeg, ffprobe, root, env)
    with running(launcher, env, admin_base, root / "restart.log"):
        assert api(admin_base, "/api/settings")["scans_paused"] is True
        assert api(tuner, "/discover.json")["DeviceID"] == device
        assert any(c["GuideName"] == "Native fixture" for c in api(tuner, "/lineup.json"))
    desktop_menu(launcher, env, admin_base, root)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="antennarr-native-") as scratch:
        smoke(args.archive.resolve(), Path(scratch))
