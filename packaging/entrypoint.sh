#!/bin/sh
set -eu
fail() { echo "Antennarr startup: $*" >&2; exit 1; }
root=${ANTENNARR_DATA:-/data}
case "$root" in /|/etc|/usr|/var|/opt|/tmp|/home|/root|/dev|/proc|/sys|'') fail 'Use a dedicated appdata directory, not a system directory';; esac
[ ! -L "$root" ] || fail 'The appdata root must not be a symbolic link'
if [ "$(id -u)" = 0 ]; then
  uid=${PUID:-10001}; gid=${PGID:-10001}
  case "$uid" in ''|*[!0-9]*) fail 'PUID must be a positive numeric user ID';; esac
  case "$gid" in ''|*[!0-9]*) fail 'PGID must be a positive numeric group ID';; esac
  [ "$uid" -gt 0 ] && [ "$gid" -gt 0 ] || fail 'PUID and PGID must be greater than zero'
  mkdir -p "$root" || fail "Cannot create data directory; check the bind mount and NAS permissions"
  # Do not follow symlinks or descend into other mounted filesystems.
  find "$root" -xdev \( -type d -o -type f \) \( ! -uid "$uid" -o ! -gid "$gid" \) -exec chown --no-dereference "$uid:$gid" {} + || fail 'Cannot set appdata ownership. Use Docker --user with a writable data directory if the NAS forbids chown.'
  exec setpriv --reuid="$uid" --regid="$gid" --clear-groups --no-new-privs /usr/bin/tini -- /usr/local/bin/antennarr "$@"
fi
if [ -n "${PUID:-}${PGID:-}" ]; then
  [ "${PUID:-$(id -u)}" = "$(id -u)" ] && [ "${PGID:-$(id -g)}" = "$(id -g)" ] || fail 'PUID/PGID differ from Docker --user. Remove PUID/PGID or use the matching IDs.'
fi
exec /usr/bin/tini -- /usr/local/bin/antennarr "$@"
