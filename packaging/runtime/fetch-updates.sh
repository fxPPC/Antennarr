#!/bin/sh
# Prepare an offline Wolfi repository containing only the pinned security updates.
set -eu
case "${1:?expected amd64 or arm64}" in
    amd64) arch=x86_64 ;;
    arm64) arch=aarch64 ;;
    *) echo 'unsupported package architecture' >&2; exit 1 ;;
esac
root=$(dirname "$0")
dest=${2:?expected output directory}
cache=$root/wolfi-updates/$arch
mkdir -p "$dest/$arch"
fetch() {
    if [ -f "$cache/$1" ]; then
        cp "$cache/$1" "$dest/$arch/$1"
    else
        curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --retry 3 \
            "https://packages.wolfi.dev/os/$arch/$1" -o "$dest/$arch/$1"
    fi
}
# apk authenticates this index with the keys in the pinned runtime, then
# validates each APK against the signed package record during installation.
fetch APKINDEX.tar.gz
: > "$dest/world"
while read -r name version amd64 arm64; do
    case "$name" in '#'*) continue ;; esac
    file=$name-$version.apk
    fetch "$file"
    digest=$amd64
    if [ "$arch" = aarch64 ]; then digest=$arm64; fi
    (cd "$dest/$arch" && printf '%s  %s\n' "$digest" "$file" | sha256sum -c -)
    printf '%s=%s\n' "$name" "$version" >> "$dest/world"
done < "$root/updates.lock"
