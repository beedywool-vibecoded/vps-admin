#!/bin/sh
# Runs a script linked with `sudo vps-admin link`, as the owner of the script file, so a
# script in someone's project folder has that person's rights (root's when root owns it).
# Only links in the root-owned folder can run: the agent cannot add any.
# Installed root-owned in /opt/vps-admin; sudoers lets the agent call it.
set -eu
dir=/etc/vps-admin/scripts
name=${1:-}
[ $# -gt 0 ] && shift
case "$name" in '' | */* | .*) echo "invalid script name" >&2; exit 64 ;; esac
link="$dir/$name"
[ -L "$link" ] || { echo "no such script: $name" >&2; exit 66; }
target=$(readlink -f "$link")
[ -f "$target" ] || { echo "the linked file is gone: $target" >&2; exit 66; }
owner=$(stat -c %U "$target")
cd "$(dirname "$target")"
if [ "$owner" = root ]; then
	exec "$target" "$@"
fi
home=$(getent passwd "$owner" | cut -d: -f6)
exec env HOME="$home" USER="$owner" runuser -u "$owner" -- "$target" "$@"
