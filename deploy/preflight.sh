#!/bin/sh
# Runs before every start of the agent (ExecStartPre). After a self-update the agent leaves
# an attempt counter; if the new binary fails to come up three times, put the previous one
# back. This lives outside the binary because a broken build cannot undo itself.
# Installed root-owned so the agent cannot rewrite it.
set -eu
bin=/opt/vps-admin/bin/vps-admin
state=/var/lib/vps-admin/update-pending
target=/var/lib/vps-admin/update-target
result=/var/lib/vps-admin/update-result

[ -f "$state" ] || exit 0
n=$(cat "$state" 2>/dev/null || echo 0)
case "$n" in *[!0-9]*|'') n=0 ;; esac
n=$((n + 1))
echo "$n" > "$state"

if [ "$n" -gt 3 ] && [ -f "$bin.prev" ]; then
	bad=$(cat "$target" 2>/dev/null || echo unknown)
	mv -f "$bin.prev" "$bin"
	rm -f "$state" "$target"
	echo "rolled back $bad" > "$result"
	echo "vps-admin: update to $bad failed to start three times, rolled back" >&2
fi
exit 0
