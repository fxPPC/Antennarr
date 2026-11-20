#!/usr/bin/env python3
"""Build the opt-in beta desktop ZIPs from an Antennarr checkout."""
import argparse
from contextlib import contextmanager, suppress
import hashlib
import os
from pathlib import Path
import plistlib
import re
import secrets
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


@contextmanager
def release_directory(path):
    # Resolve existing parent aliases (such as /tmp on macOS), but open the
    # selected directory itself without following a symlink. Hold it throughout
    # the build so renaming any ancestor cannot redirect publication.
    target = path.parent.resolve() / path.name if path.name not in ("", "..") else path.resolve()
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    directory = os.open("/", flags)
    try:
        for component in target.parts[1:]:
            name = os.path.basename(component)
            if name in ("", ".", "..") or name != component:
                raise ValueError("Expected a directory name")
            try:
                child = os.open(name, flags, dir_fd=directory)
            except FileNotFoundError:
                try:
                    os.mkdir(name, dir_fd=directory)
                except FileExistsError:
                    pass  # A concurrent builder may have just created the same directory.
                child = os.open(name, flags, dir_fd=directory)
            os.close(directory)
            directory = child
        yield directory
    finally:
        os.close(directory)


def publish(asset, output_fd):
    # Even when the build uses another filesystem, the final rename must be
    # local to the destination so readers always get a complete, verified file.
    temporary = ".antennarr-" + secrets.token_hex(16)
    flags = os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW
    fd = os.open(temporary, flags, 0o600, dir_fd=output_fd)
    try:
        with os.fdopen(fd, "wb") as dst, asset.open("rb") as src:
            shutil.copyfileobj(src, dst)
            dst.flush()
            os.fchmod(dst.fileno(), 0o644)
            os.fsync(dst.fileno())
        os.replace(temporary, asset.name, src_dir_fd=output_fd, dst_dir_fd=output_fd)
    except BaseException:
        with suppress(FileNotFoundError):
            os.unlink(temporary, dir_fd=output_fd)
        raise
    os.fsync(output_fd)


def build(target, version, output, output_fd, media):
    system, arch = target.split("-")
    mac = system == "macos"
    ext = "" if mac else ".exe"
    if mac and sys.platform != "darwin":
        raise ValueError("Build macOS desktop targets on a Mac with the Command Line Tools.")
    with tempfile.TemporaryDirectory(prefix="antennarr-package-") as work:
        stage = Path(work) / "archive"
        contents = stage / "Antennarr.app" / "Contents"
        binaries = contents / "MacOS" if mac else stage
        resources = contents / "Resources" if mac else stage
        binaries.mkdir(parents=True)
        resources.mkdir(parents=True, exist_ok=True)
        # Package only outputs of build-native-media.sh, even if its working
        # directory was reused or holds other local files.
        media = media.resolve(strict=True)
        payload = resources / "media"
        for folder, names in (("bin", ("ffmpeg" + ext, "ffprobe" + ext)),
                              ("licences", ("LICENSE.md", "COPYING.LGPLv2.1")),
                              ("source", ("SHA256SUMS", "build-native-media.sh"))):
            (payload / folder).mkdir(parents=True)
            for name in names:
                copy_media(media, payload, folder, name)
        for tool in ("ffmpeg", "ffprobe"):
            verify_machine(payload / "bin" / (tool + ext), system, arch)
        checksum = (payload / "source/SHA256SUMS").read_text(encoding="ascii").strip()
        source = re.fullmatch(r"([0-9a-f]{64})  (ffmpeg-[0-9]+(?:\.[0-9]+)*\.tar\.xz)", checksum)
        if not source:
            raise ValueError("Invalid FFmpeg source checksum record")
        copied_source = copy_media(media, payload, "source", source[2])
        if hashlib.sha256(copied_source.read_bytes()).hexdigest() != source[1]:
            raise ValueError("FFmpeg source checksum does not match the packaged archive")
        shutil.copytree(ROOT / "docs" / "licenses", resources / "licenses")
        shutil.copyfile(ROOT / "LICENSE", resources / "LICENSE")
        launcher = binaries / ("Antennarr" + ext)
        server = binaries / ("antennarr-server" + ext)
        env = dict(os.environ, GOOS="darwin" if mac else "windows", GOARCH=arch, CGO_ENABLED="0")
        flags = ["go", "build", "-trimpath", "-ldflags=-s -w"]
        if mac:
            shutil.copyfile(ASSETS / "Antennarr.icns", resources / "Antennarr.icns")
            shutil.copyfile(ASSETS / "TrayIcon.png", resources / "TrayIcon.png")
            number, beta = version[1:].split("-beta.")
            info = {"CFBundlePackageType": "APPL", "CFBundleExecutable": "Antennarr",
                    "CFBundleIdentifier": "org.antennarr.desktop.beta", "CFBundleName": "Antennarr",
                    "CFBundleDisplayName": "Antennarr", "CFBundleIconFile": "Antennarr.icns",
                    "CFBundleInfoDictionaryVersion": "6.0", "CFBundleShortVersionString": number,
                    "CFBundleVersion": number + "b" + beta, "LSMinimumSystemVersion": "13.0",
                    "NSLocalNetworkUsageDescription": "Antennarr connects to channel sources you add on your local network.",
                    "LSUIElement": True, "NSHighResolutionCapable": True}
            (contents / "Info.plist").write_bytes(plistlib.dumps(info))
            env.update(CGO_ENABLED="1", MACOSX_DEPLOYMENT_TARGET="13.0")
            launcher_package = "./cmd/antennarr"
            flags += ["-tags=desktop"]
        else:
            shutil.copyfile(ASSETS / "Antennarr.ico", resources / "Antennarr.ico")
            launcher_package = "./packaging/native/desktop"
            subprocess.run(flags + ["-o", str(server), "./cmd/antennarr"], cwd=ROOT, env=env, check=True)
            flags[-1] += " -H=windowsgui"
        subprocess.run(flags + ["-o", str(launcher), launcher_package], cwd=ROOT, env=env, check=True)
        for binary in ([launcher] if mac else [launcher, server]):
            verify_machine(binary, system, arch)
        readme = (ASSETS / "README.txt").read_text().replace("@VERSION@", version)
        readme = readme.replace("@START@", (
            "Drag Antennarr to Applications, eject the disk image, then open Antennarr\n"
            "from Applications. Its menu is in the Mac menu bar.\n"
            "macOS 13 or later is required. This beta has no Apple Developer ID signature or notarisation."
            if mac else "Install Antennarr, then open it from Start. Its menu is in the\n"
            "Windows notification area (check hidden icons). This beta has no publisher signature."
        )).replace("@DATA@", "~/Library/Application Support/Antennarr Beta" if mac else r"%LOCALAPPDATA%\Antennarr Beta")
        firewall = ("If macOS asks, allow incoming connections for Antennarr so Plex can connect."
                    if mac else "Choose the private-network firewall option in the installer for Plex on your LAN.")
        cli = ("/Applications/Antennarr.app/Contents/MacOS/Antennarr --serve runs the server in the foreground."
               if mac else "antennarr-server.exe runs the server in the foreground.")
        readme = readme.replace("@FIREWALL@", firewall).replace("@CLI@", cli)
        (stage / "README.txt").write_text(readme, encoding="utf-8")
        if mac:
            (resources / "README.txt").write_text(readme, encoding="utf-8")
        archive = Path(work) / f"antennarr-{version}-{target}.zip"
        if mac:
            # Seal the complete bundle, including the bundled helpers. Ad-hoc
            # signing checks integrity without claiming a Developer ID identity.
            for tool in (resources / "media/bin/ffmpeg", resources / "media/bin/ffprobe"):
                subprocess.run(["codesign", "--force", "--sign", "-", "--timestamp=none", str(tool)], check=True)
            app = contents.parent
            subprocess.run(["codesign", "--force", "--sign", "-", "--timestamp=none", str(app)], check=True)
            subprocess.run(["codesign", "--verify", "--deep", "--strict", str(app)], check=True)

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
        publish(archive, output_fd)
        print(output / archive.name, flush=True)
        if mac:
            layout = Path(work) / "Drag to Applications"
            layout.mkdir()
            (stage / "Antennarr.app").rename(layout / "Antennarr.app")
            (layout / "Applications").symlink_to("/Applications")
            shutil.copyfile(ASSETS / "finder-layout.bin", layout / ".DS_Store")
            shutil.copyfile(stage / "README.txt", layout / "Read Me.txt")
            image = archive.with_suffix(".dmg")
            subprocess.run(["hdiutil", "create", "-volname", "Antennarr Beta", "-fs", "HFS+",
                            "-format", "UDZO", "-srcfolder", str(layout), str(image)], check=True)
            subprocess.run(["hdiutil", "verify", "-quiet", str(image)], check=True)
            publish(image, output_fd)
            print(output / image.name, flush=True)


def copy_media(media, payload, folder, name):
    path = media / folder / name
    if path.resolve(strict=True) != path or not path.is_file():
        raise ValueError(f"Expected a regular media file: {path}")
    return Path(shutil.copy2(path, payload / folder / name))


def release_version(tag):
    match = re.fullmatch(r"v1\.1\.(0|[1-9][0-9]*)-beta\.([1-9][0-9]*)", tag)
    if not match:
        raise ValueError("expected a 1.1 beta release tag, for example v1.1.0-beta.1")
    # Filenames and bundle metadata use the two release numbers in canonical form.
    return f"v1.1.{int(match[1])}-beta.{int(match[2])}"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version")
    parser.add_argument("output", type=Path)
    parser.add_argument("--target", choices=TARGETS)
    parser.add_argument("--media-dir", type=Path, required=True,
                        help="verified target payload, or a directory containing a payload for each target")
    args = parser.parse_args()
    try:
        version = release_version(args.version)
    except ValueError as error:
        parser.error(str(error))
    with release_directory(args.output) as output_fd:
        for target in TARGETS:
            if args.target and target != args.target:
                continue
            media = args.media_dir / target
            if args.target and not media.is_dir():
                media = args.media_dir
            build(target, version, args.output, output_fd, media)


if __name__ == "__main__":
    main()
