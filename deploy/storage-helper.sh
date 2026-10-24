#!/bin/sh
# Mounts the panel's Google Drive (rclone remote "vpsadmin") as the vps-admin user, with
# a fixed systemd unit so the mount comes back on boot.
# Installed root-owned in /opt/vps-admin; sudoers lets the agent call it.
#
#   storage-helper mount vpsadmin    write the unit, enable and start it
#   storage-helper umount vpsadmin   stop it and remove the unit
set -eu

name=${2:-}
[ "$name" = vpsadmin ] || { echo "invalid storage name" >&2; exit 64; }

target=/srv/storage/$name
conf=/var/lib/vps-admin/rclone.conf
unit=/etc/systemd/system/rclone-$name.service

find_fuse() {
	command -v fusermount3 2>/dev/null || command -v fusermount 2>/dev/null || true
}

case "${1:-}" in
mount)
	command -v rclone >/dev/null 2>&1 ||
		{ echo "rclone is not installed: rerun install.sh" >&2; exit 69; }
	[ -f "$conf" ] ||
		{ echo "no Drive is connected yet: save the token in the panel first" >&2; exit 66; }
	[ -e /dev/fuse ] ||
		{ echo "/dev/fuse is missing: this VPS cannot mount FUSE filesystems" >&2; exit 69; }
	rclone_bin=$(command -v rclone)
	fuse_bin=$(find_fuse)
	[ -n "$fuse_bin" ] || { echo "fusermount is missing: install fuse3" >&2; exit 69; }
	# allow_other is what lets the panel and the containers read the mount.
	if ! grep -qs '^[[:space:]]*user_allow_other' /etc/fuse.conf; then
		printf 'user_allow_other\n' >>/etc/fuse.conf
	fi
	install -d -o vps-admin -g vps-admin -m 755 "$target"
	# rclone writes its VFS cache there: without HOME the default would be unwritable.
	install -d -o vps-admin -g vps-admin -m 700 /var/lib/vps-admin/rclone-cache
	systemctl stop "rclone-$name" >/dev/null 2>&1 || true
	"$fuse_bin" -uz "$target" 2>/dev/null || true
	cat >"$unit" <<EOF
[Unit]
Description=VPS Admin storage: Google Drive ($name)
Documentation=https://github.com/beedywool/vps-admin
After=network-online.target
Wants=network-online.target

[Service]
User=vps-admin
Group=vps-admin
ExecStart=$rclone_bin mount $name: $target --config $conf --cache-dir /var/lib/vps-admin/rclone-cache --allow-other --vfs-cache-mode writes --dir-cache-time 1h --poll-interval 0 --umask 002 --log-level INFO
ExecStop=-$fuse_bin -uz $target
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
	systemctl daemon-reload
	systemctl enable --now "rclone-$name"
	;;
umount)
	systemctl disable --now "rclone-$name" >/dev/null 2>&1 || true
	rm -f "$unit"
	systemctl daemon-reload
	fuse_bin=$(find_fuse)
	[ -z "$fuse_bin" ] || "$fuse_bin" -uz "$target" 2>/dev/null || true
	;;
*)
	echo "usage: storage-helper mount|umount vpsadmin" >&2
	exit 64
	;;
esac
