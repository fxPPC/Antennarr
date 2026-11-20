#!/bin/sh
# Build standalone LGPL media tools from unmodified upstream source. Keep the
# corresponding source, notices, and build recipe with each native download.
set -eu
target=${1:?Usage: build-native-media.sh TARGET OUTPUT_DIR}
mkdir -p "${2:?Choose a new media directory}"
output=$(cd "$2" && pwd)
version=9.1
source_name=ffmpeg-$version.tar.xz
source_hash=1a4d394ae63d190a526cd052eb4415c156b5c5e2df37efab41a7de0b8fde2433
recipe=$(cd "$(dirname "$0")" && pwd)/build-native-media.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"
verify() {
    python3 -c 'import hashlib, pathlib, sys; digest=hashlib.sha256(pathlib.Path(sys.argv[1]).read_bytes()).hexdigest(); sys.exit(0 if digest==sys.argv[2] else "Unexpected SHA-256 for "+sys.argv[1])' "$1" "$2"
}
if [ -n "${FFMPEG_SOURCE_ARCHIVE:-}" ]; then
    cp "$FFMPEG_SOURCE_ARCHIVE" "$source_name"
else
    curl --fail --location --silent --show-error --retry 3 "https://ffmpeg.org/releases/$source_name" -o "$source_name"
fi
verify "$source_name" "$source_hash"
tar -xf "$source_name"
case "$target" in
    macos-amd64|macos-arm64)
        case "$target" in macos-amd64) cpu=x86_64 ;; *) cpu=arm64 ;; esac
        export MACOSX_DEPLOYMENT_TARGET=12.0
        set -- --target-os=darwin "--arch=$cpu" --cc=clang "--extra-cflags=-arch $cpu" "--extra-ldflags=-arch $cpu"
        if [ "$(uname -m)" != "$cpu" ]; then set -- "$@" --enable-cross-compile; fi
        extension=
        ;;
    windows-amd64|windows-arm64)
        case "$target" in windows-amd64) cpu=x86_64 ;; *) cpu=aarch64 ;; esac
        archive=llvm-mingw-20261117-ucrt-ubuntu-22.04-x86_64.tar.xz
        curl --fail --location --silent --show-error --retry 3 "https://github.com/mstorsjo/llvm-mingw/releases/download/20261117/$archive" -o "$archive"
        verify "$archive" e7a15a49a2ea95e9f3d8a9d0cfabe5052a4b10fb4777cc54de5a8c27b64bb701
        tar -xf "$archive"
        export PATH="$work/${archive%.tar.xz}/bin:$PATH"
        prefix=$cpu-w64-mingw32-
        set -- --target-os=mingw32 "--arch=$cpu" --enable-cross-compile "--cross-prefix=$prefix" "--cc=${prefix}clang" "--cxx=${prefix}clang++" "--extra-ldflags=-static"
        extension=.exe
        ;;
    *) echo "Unsupported media platform: $target" >&2; exit 1 ;;
esac
cd "ffmpeg-$version"
./configure "$@" --disable-autodetect --disable-shared --enable-static --disable-debug \
    --disable-doc --disable-ffplay --disable-devices --enable-indev=lavfi \
    --disable-encoders --enable-encoder=aac,ac3,mp2,mpeg2video,pcm_s16le,rawvideo,wrapped_avframe
make -j"${BUILD_JOBS:-4}" "ffmpeg$extension" "ffprobe$extension"
mkdir -p "$output/bin" "$output/licences" "$output/source"
cp "ffmpeg$extension" "ffprobe$extension" "$output/bin/"
cp LICENSE.md COPYING.LGPLv2.1 "$output/licences/"
cp "$work/$source_name" "$recipe" "$output/source/"
printf '%s  %s\n' "$source_hash" "$source_name" > "$output/source/SHA256SUMS"
