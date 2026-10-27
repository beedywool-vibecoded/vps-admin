#!/bin/sh
# Runs a script linked with `sudo vps-admin link` as the owner of the file, so it gets
# that person's rights. Only links in the root-owned folder can run.
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
# Anyone able to edit the file or swap it in its folder could run code as its owner.
for f in "$target" "$(dirname "$target")"; do
	if [ -n "$(find "$f" -maxdepth 0 -perm -0002 ! -perm -1000)" ]; then
		echo "refused: $f is writable by every user on the server. Fix it with: chmod o-w '$f'" >&2
		exit 77
	fi
done
owner=$(stat -c %U "$target")
cd "$(dirname "$target")"
if [ "$owner" = root ]; then
	exec "$target" "$@"
fi
home=$(getent passwd "$owner" | cut -d: -f6)
exec env HOME="$home" USER="$owner" runuser -u "$owner" -- "$target" "$@"
