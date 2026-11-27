# Build Antennarr 1.1

Use the same tag on each build machine. The example uses the published 1.1 preview. Keep build output outside the checkout so it stays out of the Docker build context.

```sh
git clone --branch v1.1.0-beta.2 --depth 1 https://github.com/fxPPC/Antennarr.git
cd Antennarr
version=$(git describe --tags --exact-match)
revision=$(git rev-parse HEAD)
out=$(mktemp -d)
```

Use Go 1.27.4 and Python 3.9 or later. Go verifies module downloads against `go.sum`. The desktop media script verifies the SHA-256 of FFmpeg 9.1 and, on Linux, the Windows cross-compiler. To build with an existing copy of the FFmpeg source, set `FFMPEG_SOURCE_ARCHIVE` to the absolute path of `ffmpeg-9.1.tar.xz`. The same digest check applies.

## Linux Docker image

The Dockerfile pins the Go builder and Wolfi base by digest. It restores the release’s SHA-256-verified Wolfi package layer for each architecture, including FFmpeg 9.1-r0. Keep the release assets `wolfi-runtime-linux-amd64.tar.gz` and `wolfi-runtime-linux-arm64.tar.gz` with the source when archiving a release. [Runtime layer digests](../packaging/runtime/lock.json)

A normal `docker build -t antennarr:local .` fetches the runtime layer automatically. To cache it first, run `go run ./packaging/runtime -arch amd64 -out packaging/runtime/wolfi-runtime-linux-amd64.tar.gz` from the checkout, substituting `arm64` in both places for ARM64. The builder still needs the pinned base images and Go modules.

For the release checks, run the following on a disposable Linux runner of each target architecture. CI uses Ubuntu 24.04, Docker Buildx, Skopeo, and FFmpeg.

```sh
arch=amd64  # use arm64 on ARM64
builder=$(docker buildx create --driver docker-container)
docker buildx build --builder "$builder" --platform "linux/$arch" --pull \
  --provenance=false --sbom=false \
  --build-arg "VERSION=$version" --build-arg "REVISION=$revision" \
  --output "type=oci,dest=$out/candidate.tar" .
docker buildx rm "$builder"
skopeo copy "oci-archive:$out/candidate.tar" docker-daemon:antennarr:candidate
export ANTENNARR_TEST_IMAGE=antennarr:candidate
python3 packaging/test-permissions.py
export ANTENNARR_TEST_FIXTURES="$out/fixtures"
sh scripts/fixtures.sh "$ANTENNARR_TEST_FIXTURES"
sh packaging/test-runtime-media.sh
```

The release workflow records the manifest digest of each tested architecture, scans each candidate, and publishes their multi-platform image. Each release gets a version tag. The `testing` tag follows published previews. The stable 1.0 line remains the default. [Release procedure](RELEASING.md)

## macOS disk image

Use a Mac with Xcode Command Line Tools, make, curl, and xz-capable tar. Install NASM for Intel builds. CI builds on macOS 15 and tests the installed app on macOS 15 and 26 on both Apple silicon and Intel. Set the target to `macos-arm64` or `macos-amd64` to match the Mac running the test.

```sh
target=macos-arm64
sh packaging/build-native-media.sh "$target" "$out/$target/media"
python3 packaging/build-native.py "$version" "$out/downloads" \
  --target "$target" --media-dir "$out/$target/media"
python3 packaging/test-native.py "$out/downloads/antennarr-$version-$target.dmg"
```

The public download is the DMG. The test mounts it read-only, copies the app, ejects the image, and runs the installed copy with its bundled FFmpeg and FFprobe.

## Windows installer

Build the Windows payload on Ubuntu 24.04 x86-64 with make, curl, and xz-capable tar. Install NASM for the x64 target. The media script downloads LLVM-MinGW 20261117 and verifies its SHA-256. Run once for `windows-amd64` and once for `windows-arm64`, using a separate output directory for each build.

```sh
target=windows-amd64
sh packaging/build-native-media.sh "$target" "$out/$target/media"
python3 packaging/build-native.py "$version" "$out/downloads" \
  --target "$target" --media-dir "$out/$target/media"
```

Copy the ZIP into `downloads` on Windows. From the same tagged checkout, run PowerShell with the matching architecture. CI compiles both installers on Windows Server 2025. The script verifies and unpacks Inno Setup 6.7.3 into a temporary directory and prints the final installer's SHA-256.

```powershell
$version = git describe --tags --exact-match
$arch = 'amd64'  # use arm64 for the ARM64 payload
./packaging/build-windows-installer.ps1 -Version $version -Arch $arch `
  -PayloadZip "downloads/antennarr-$version-windows-$arch.zip" -OutputDir installers
```

Test the resulting installer on Windows with a clean user profile and the matching CPU. CI covers Windows 11 on ARM64 and Windows Server 2022 and 2025 on x64.

```powershell
python packaging/test-native.py "installers/antennarr-$version-windows-$arch-setup.exe"
```

The desktop tests play generated audio and video through the tuner, exercise the native menu, and verify saved settings after restart. The Windows test also uninstalls the app and checks that its data survives. GitHub-hosted installer tests set `ANTENNARR_TEST_FIREWALL=1` to exercise the platform firewall on disposable machines. [Native build and test workflow](../.github/workflows/native-downloads.yml)

## Verify a download

Download `SHA256SUMS` from the same GitHub release as the installer. Compare its entry with `shasum -a 256 <file>` on macOS or `Get-FileHash -Algorithm SHA256 <file>` in PowerShell. For Docker, the release records the published multi-platform digest in `IMAGE-DIGEST.txt`.

Those checksums identify the published files. Repeating a build checks the same source and dependencies. Timestamps, SDK revisions, and installer metadata can change the bytes of a local rebuild. [Release assets](https://github.com/fxPPC/Antennarr/releases)
