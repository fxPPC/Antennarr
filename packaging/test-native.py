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


def free_ports():
    # Hold all three until selected so the OS cannot return the same port twice.
    with contextlib.ExitStack() as stack:
        ports = []
        for _ in range(3):
            sock = stack.enter_context(socket.socket())
            sock.bind(("127.0.0.1", 0))
            ports.append(sock.getsockname()[1])
        return ports


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
        process = subprocess.Popen([executable], cwd=executable.parent, env=env,
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
            result.append(data[name:end].decode("ascii").lower())
            pos += 20
        return result


def check_native_files(package, executables, arch):
    if os.name == "nt":
        system = Path(os.environ["SystemRoot"]) / "System32"
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


def firewall_state(server):
    # Only enumerate the optional rule for this unique, disposable installation path.
    script = """
        $p = New-Object -ComObject HNetCfg.FwPolicy2
        $rules = @($p.Rules | Where-Object {
            $_.Name -eq 'Antennarr Beta LAN tuner' -and $_.ApplicationName -eq $env:TEST_TUNER_PROGRAM
        } | Select-Object Enabled,Direction,Action,Profiles,Protocol,LocalPorts,RemoteAddresses)
        @{privateEnabled=$p.FirewallEnabled(2); activeProfiles=$p.CurrentProfileTypes; rules=$rules} | ConvertTo-Json -Compress -Depth 3
    """
    out = subprocess.check_output(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script],
                                  env={**os.environ, "TEST_TUNER_PROGRAM": str(server)}, text=True, timeout=30)
    return json.loads(out)


def firewall_requested():
    requested = os.environ.get("ANTENNARR_TEST_FIREWALL") == "1"
    if requested and os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted":
        raise RuntimeError("Firewall changes are restricted to disposable GitHub-hosted runners.")
    return requested


@contextlib.contextmanager
def mac_firewall(server):
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
    server = str(server.resolve())
    assert server not in read("--listapps")
    restore_off = "disabled" in state.lower()
    try:
        configure("--setglobalstate", "on")
        assert "enabled" in read("--getglobalstate").lower()
        configure("--add", server)
        configure("--unblockapp", server)
        assert server in read("--listapps")
        allowed = read("--getappblocked", server)
        assert re.search(r"permit|allow|not blocked|unblocked", allowed, re.I), allowed
        yield
    finally:
        try:
            if server in read("--listapps"):
                configure("--remove", server)
            assert server not in read("--listapps")
        finally:
            if restore_off:
                configure("--setglobalstate", "off")


def set_private_firewall(enabled):
    subprocess.run(["powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
                    "Set-NetFirewallProfile -Profile Private -Enabled " + ("True" if enabled else "False")],
                   check=True, timeout=30)


def verify_windows_rule(server):
    state = firewall_state(server)
    assert state["privateEnabled"]
    [rule] = state["rules"]
    assert rule["Enabled"] and rule["Profiles"] == 2 and rule["Direction"] == 1 and rule["Action"] == 1
    assert rule["Protocol"] == 6 and set(rule["LocalPorts"].split(",")) == {"9192", "9200-9207"}
    scopes = {x.casefold() for x in rule["RemoteAddresses"].split(",")}
    assert scopes and scopes <= {"localsubnet", "localsubnet4", "localsubnet6"}, scopes


@contextlib.contextmanager
def installed(archive, root):
    package = root / "Test Applications"
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as z:
            z.extractall(package)
            if platform.system() == "Darwin":
                # Python drops ZIP permission bits. Finder and macOS unzip retain them.
                for info in z.infolist():
                    if not info.is_dir() and info.external_attr >> 16 & 0o111:
                        (package / info.filename).chmod(0o755)
        yield package
    elif archive.suffix == ".dmg":
        mount = root / "Mounted image"
        mount.mkdir()
        subprocess.run(["hdiutil", "verify", "-quiet", archive], check=True, timeout=100)
        subprocess.run(["hdiutil", "attach", "-quiet", "-readonly", "-nobrowse", "-noautoopen",
                        "-mountpoint", mount, archive], check=True, timeout=90)
        try:
            assert os.statvfs(mount).f_flag & os.ST_RDONLY
            assert (mount / "Applications").is_symlink() and os.readlink(mount / "Applications") == "/Applications"
            assert (mount / "Read Me.txt").is_file()
            package.mkdir()
            subprocess.run(["ditto", mount / "Antennarr.app", package / "Antennarr.app"], check=True, timeout=90)
        finally:
            subprocess.run(["hdiutil", "detach", "-quiet", mount], check=True, timeout=60)
        with mac_firewall(package / "Antennarr.app" / "Contents" / "MacOS" / "antennarr-server"):
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
        flags = ["/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART"]
        server = package / "antennarr-server.exe"
        state = firewall_state(server)
        print("Windows private firewall enabled:", state["privateEnabled"], "active profile mask:", state["activeProfiles"])
        assert not state["rules"]
        uninstaller = package / "unins000.exe"
        active_firewall_test = firewall_requested()
        if active_firewall_test and not ctypes.windll.shell32.IsUserAnAdmin():
            raise RuntimeError("Testing the private firewall rule requires an elevated disposable runner.")
        try:
            subprocess.run([archive, *flags, "/TASKS=", "/DIR=" + str(package)], check=True, timeout=120)
            assert uninstaller.is_file() and not firewall_state(server)["rules"]
            if active_firewall_test:
                if not state["privateEnabled"]:
                    set_private_firewall(True)
                subprocess.run([archive, *flags, "/TASKS=lanfirewall", "/DIR=" + str(package)],
                               check=True, timeout=120)
                verify_windows_rule(server)
            yield package
        finally:
            try:
                if uninstaller.exists():
                    subprocess.run([uninstaller, *flags], check=True, timeout=100)
                assert not server.exists() and not uninstaller.exists()
                if active_firewall_test and firewall_state(server)["rules"]:
                    subprocess.run(["netsh.exe", "advfirewall", "firewall", "delete", "rule",
                                    "name=Antennarr Beta LAN tuner", "program=" + str(server), "dir=in"],
                                   check=True, timeout=20)
                    raise AssertionError("Uninstall left its test firewall rule behind")
            finally:
                if active_firewall_test and not state["privateEnabled"]:
                    set_private_firewall(False)
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
        bindir = package / "Antennarr.app" / "Contents" / "MacOS"
        launcher, server = bindir / "Antennarr", bindir / "antennarr-server"
        media = bindir.parent / "Resources" / "media" / "bin"
        ffmpeg, ffprobe = media / "ffmpeg", media / "ffprobe"
    else:
        launcher, server = package / "Antennarr.exe", package / "antennarr-server.exe"
        media = package / "media" / "bin"
        ffmpeg, ffprobe = media / "ffmpeg.exe", media / "ffprobe.exe"
    for path in [launcher, server, ffmpeg, ffprobe]:
        assert path.is_file() and os.access(path, os.X_OK), f"Missing executable {path}"

    check_native_files(package, [launcher, server, ffmpeg, ffprobe], arch)
    primary, admin, extra = free_ports()
    tuner_host = "127.0.0.1"
    if firewall_requested():
        # UDP connect selects a local route without sending packets to the destination.
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as route:
            route.connect(("192.0.2.1", 9))
            tuner_host = route.getsockname()[0]
        address = ipaddress.ip_address(tuner_host)
        assert address.is_private and not address.is_loopback, f"Expected a private runner address: {tuner_host}"
        primary, admin, extra = 9192, 39192, 9200
        print("Tuning through the runner's LAN address with the firewall enabled (no off-host Plex client).")
    base = f"http://{tuner_host}:{primary}"
    admin_base = f"http://127.0.0.1:{admin}"
    env = {k: v for k, v in os.environ.items() if not k.upper().startswith("ANTENNARR_")}
    if os.name == "nt":
        windows = Path(env["SystemRoot"])
        env["PATH"] = os.pathsep.join(map(str, [windows / "System32", windows]))
    else:
        env["PATH"] = "/usr/bin:/bin"
        env = {k: v for k, v in env.items() if not k.startswith("DYLD_")}
    for name in ["ffmpeg", "ffprobe"]:
        assert not shutil.which(name, path=env["PATH"]), f"{name} found on isolated test PATH"
    for tool in [ffmpeg, ffprobe]:
        subprocess.run([tool, "-version"], stdout=subprocess.DEVNULL, check=True, timeout=10, env=env)
    env.update(ANTENNARR_HEADLESS="1", ANTENNARR_DATA=str(root / "Application Data"),
               ANTENNARR_LISTEN=f"{tuner_host}:{primary}", ANTENNARR_ADMIN_LISTEN=f"127.0.0.1:{admin}",
               ANTENNARR_TUNER_PORTS=str(extra), ANTENNARR_BASE_URL=base, ANTENNARR_FIXTURES=str(root))
    if firewall_requested():
        del env["ANTENNARR_ADMIN_LISTEN"]  # Exercise the desktop launcher’s default loopback administration.
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
        profile = api(admin_base, "/api/profiles", {"preset_id": "in-mumbai"})
        api(admin_base, "/api/profiles/" + profile["id"], {"enabled": True})
        second = api(admin_base, "/api/profiles", {"preset_id": "de-berlin"})
        api(admin_base, "/api/profiles/" + second["id"], {"enabled": True})
        profiles = api(admin_base, "/api/profiles")
        saved = next(p for p in profiles if p["id"] == profile["id"])
        assert {p["tuner_url"] for p in profiles} == {base, f"http://{tuner_host}:{extra}"}
        for p in profiles:
            url = p["tuner_url"]
            tuner_only(url)
            assert api(url, "/discover.json")["FriendlyName"] == p["name"]
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
