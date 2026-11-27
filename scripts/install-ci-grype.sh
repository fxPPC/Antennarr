#!/bin/sh
# Seed the GitHub Actions tool cache used by anchore/scan-action. The tagged
# release archives can be fetched directly even if GitHub's tag resolver is down.
set -eu
version=0.121.3
case "${RUNNER_ARCH:?}" in
  X64)
    asset=amd64
    cache_arch=x64
    digest=e9da18cc1429e786722e32161288103600efffd8e483c90761f90485bd1bec85
    ;;
  ARM64)
    asset=arm64
    cache_arch=arm64
    digest=b7815598d48da78bfd16f5b98ef28e1bb09c03d34fc8c3e350f99b6ccb3c7cba
    ;;
  *) echo "Unsupported Linux runner architecture: $RUNNER_ARCH" >&2; exit 1 ;;
esac

cache_dir="${RUNNER_TOOL_CACHE:?}/grype/$version/$cache_arch"
mkdir -p "$cache_dir"
work_dir=$(mktemp -d "$cache_dir/.install-XXXXXX")
trap 'rm -rf "$work_dir"' EXIT
archive="grype_${version}_linux_${asset}.tar.gz"
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
  --connect-timeout 15 --max-time 180 --retry 3 \
  "https://github.com/anchore/grype/releases/download/v$version/$archive" -o "$work_dir/$archive"
printf '%s  %s\n' "$digest" "$work_dir/$archive" | sha256sum --check -
tar -xOzf "$work_dir/$archive" grype > "$work_dir/grype"
chmod 755 "$work_dir/grype"
mv -f "$work_dir/grype" "$cache_dir/grype"
touch "$cache_dir.complete"
printf 'version=v%s\n' "$version" >> "${GITHUB_OUTPUT:?}"
