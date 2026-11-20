#!/usr/bin/env python3
"""Build the opt-in beta desktop ZIPs from an Antennarr checkout."""
import argparse
import os
from pathlib import Path
import plistlib
import re
import shutil
import stat
import struct
import subprocess
import sys
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
ASSETS = ROOT / "packaging" / "native"
TARGETS = [f"{system}-{arch}" for system in ("macos", "windows") for arch in ("amd64", "arm64")]


def verify_machine(path, system, arch):
    with path.open("rb") as f:
        if system == "macos":
            magic, cpu = struct.unpack("<II", f.read(8))
            valid = magic == 0xFEEDFACF and cpu == {"amd64": 0x1000007, "arm64": 0x100000C}[arch]
        else:
            if f.read(2) != b"MZ":
                raise ValueError(f"{path} is not a Windows executable")
            f.seek(0x3C)
            f.seek(struct.unpack("<I", f.read(4))[0])
            valid = f.read(4) == b"PE\0\0" and struct.unpack("<H", f.read(2))[0] == {"amd64": 0x8664, "arm64": 0xAA64}[arch]
    if not valid:
        raise ValueError(f"{path} has the wrong architecture for {system}-{arch}")


def build(target, version, output, media):
    system, arch = target.split("-")
    mac = system == "macos"
    ext = "" if mac else ".exe"
    for tool in ("ffmpeg", "ffprobe"):
        verify_machine(media / "bin" / (tool + ext), system, arch)
    for required in ("licences", "source"):
        if not (media / required).is_dir() or not any((media / required).iterdir()):
            raise ValueError(f"Missing packaged media {required} in {media}")

    if mac and sys.platform != "darwin":
        raise ValueError("Build macOS desktop targets on a Mac with the Command Line Tools.")
    with tempfile.TemporaryDirectory(prefix="antennarr-package-") as work:
        stage = Path(work) / "archive"
        contents = stage / "Antennarr.app" / "Contents"
        binaries = contents / "MacOS" if mac else stage
        resources = contents / "Resources" if mac else stage
        binaries.mkdir(parents=True)
        resources.mkdir(parents=True, exist_ok=True)
        for entry in media.rglob("*"):
            if entry.is_symlink():
                raise ValueError(f"Media payload contains a symbolic link: {entry}")
        shutil.copytree(media, resources / "media")
        shutil.copytree(ROOT / "docs" / "licenses", resources / "licenses")
        shutil.copyfile(ROOT / "LICENSE", resources / "LICENSE")
        launcher = binaries / ("Antennarr" + ext)
        server = binaries / ("antennarr-server" + ext)
        env = dict(os.environ, GOOS="darwin" if mac else "windows", GOARCH=arch, CGO_ENABLED="0")
        flags = ["go", "build", "-trimpath", "-ldflags=-s -w"]
        subprocess.run(flags + ["-o", str(server), "./cmd/antennarr"], cwd=ROOT, env=env, check=True)
        if mac:
            shutil.copyfile(ASSETS / "Antennarr.icns", resources / "Antennarr.icns")
            shutil.copyfile(ASSETS / "TrayIcon.png", resources / "TrayIcon.png")
            number, beta = version[1:].split("-beta.")
            info = {"CFBundlePackageType": "APPL", "CFBundleExecutable": "Antennarr",
                    "CFBundleIdentifier": "org.antennarr.desktop.beta", "CFBundleName": "Antennarr",
                    "CFBundleDisplayName": "Antennarr", "CFBundleIconFile": "Antennarr.icns",
                    "CFBundleInfoDictionaryVersion": "6.0", "CFBundleShortVersionString": number,
                    "CFBundleVersion": number + "b" + beta, "LSMinimumSystemVersion": "13.0",
                    "LSUIElement": True, "NSHighResolutionCapable": True}
            (contents / "Info.plist").write_bytes(plistlib.dumps(info))
            env.update(CGO_ENABLED="1", MACOSX_DEPLOYMENT_TARGET="13.0")
        else:
            shutil.copyfile(ASSETS / "Antennarr.ico", resources / "Antennarr.ico")
            flags[-1] += " -H=windowsgui"
        subprocess.run(flags + ["-o", str(launcher), "./packaging/native/desktop"], cwd=ROOT, env=env, check=True)
        for binary in (launcher, server):
            verify_machine(binary, system, arch)
        readme = (ASSETS / "README.txt").read_text().replace("@VERSION@", version)
        readme = readme.replace("@START@", (
            "Drag Antennarr to Applications, eject the disk image, then open Antennarr\n"
            "from Applications. Its menu is in the Mac menu bar.\n"
            "macOS 13 or later is required. This beta has no Apple Developer ID signature or notarisation."
            if mac else "Install Antennarr, then open it from Start. Its menu is in the\n"
            "Windows notification area (check hidden icons). This beta has no publisher signature."
        )).replace("@DATA@", "~/Library/Application Support/Antennarr Beta" if mac else r"%LOCALAPPDATA%\Antennarr Beta")
        firewall = ("If macOS asks, allow incoming connections to antennarr-server for Plex."
                    if mac else "Choose the private-network firewall option in the installer for Plex on your LAN.")
        readme = readme.replace("@FIREWALL@", firewall)
        (stage / "README.txt").write_text(readme, encoding="utf-8")
        if mac:
            (resources / "README.txt").write_text(readme, encoding="utf-8")
        final = output / f"antennarr-{version}-{target}.zip"
        if mac:
            # Seal the complete bundle, including the bundled helpers. Ad-hoc
            # signing checks integrity without claiming a Developer ID identity.
            for tool in (server, resources / "media/bin/ffmpeg", resources / "media/bin/ffprobe"):
                subprocess.run(["codesign", "--force", "--sign", "-", "--timestamp=none", str(tool)], check=True)
            app = contents.parent
            subprocess.run(["codesign", "--force", "--sign", "-", "--timestamp=none", str(app)], check=True)
            subprocess.run(["codesign", "--verify", "--deep", "--strict", str(app)], check=True)

        # Write beside the destination so a failed build never leaves a partial release asset.
        with tempfile.NamedTemporaryFile(dir=output, prefix=".antennarr-", suffix=".zip", delete=False) as tmp:
            archive = Path(tmp.name)
        try:
            with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=6) as z:
                for file in sorted(stage.rglob("*")):
                    if not file.is_file():
                        continue
                    item = zipfile.ZipInfo(file.relative_to(stage).as_posix())
                    item.create_system = 3
                    mode = 0o755 if file in (server, launcher) or file.parent == resources / "media" / "bin" else 0o644
                    item.external_attr = (stat.S_IFREG | mode) << 16
                    item.compress_type = zipfile.ZIP_DEFLATED
                    with file.open("rb") as src, z.open(item, "w") as dest:
                        shutil.copyfileobj(src, dest)
            with zipfile.ZipFile(archive) as z:
                bad_entry = z.testzip()
                if bad_entry:
                    raise ValueError(f"ZIP verification failed for {bad_entry}")
            archive.chmod(0o644)
            archive.replace(final)
            print(final, flush=True)
        finally:
            archive.unlink(missing_ok=True)
        if mac:
            layout = Path(work) / "Drag to Applications"
            layout.mkdir()
            (stage / "Antennarr.app").rename(layout / "Antennarr.app")
            (layout / "Applications").symlink_to("/Applications")
            shutil.copyfile(ASSETS / "finder-layout.bin", layout / ".DS_Store")
            shutil.copyfile(stage / "README.txt", layout / "Read Me.txt")
            dmg = output / (final.stem + ".dmg")
            with tempfile.TemporaryDirectory(dir=output, prefix=".antennarr-dmg-") as temp:
                image = Path(temp) / dmg.name
                subprocess.run(["hdiutil", "create", "-volname", "Antennarr Beta", "-fs", "HFS+",
                                "-format", "UDZO", "-srcfolder", str(layout), str(image)], check=True)
                subprocess.run(["hdiutil", "verify", "-quiet", str(image)], check=True)
                image.replace(dmg)
            print(dmg, flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version")
    parser.add_argument("output", type=Path)
    parser.add_argument("--target", choices=TARGETS)
    parser.add_argument("--media-dir", type=Path, required=True,
                        help="verified target payload, or a directory containing a payload for each target")
    args = parser.parse_args()
    if not re.fullmatch(r"v1\.1\.\d+-beta\.\d+", args.version):
        parser.error("expected a beta release tag, for example v1.1.0-beta.1")
    args.output.mkdir(parents=True, exist_ok=True)
    for target in [args.target] if args.target else TARGETS:
        media = args.media_dir / target
        if args.target and not media.is_dir():
            media = args.media_dir
        build(target, args.version, args.output, media)


if __name__ == "__main__":
    main()
