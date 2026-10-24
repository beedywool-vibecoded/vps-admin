#!/bin/sh
# Mounts the panel's Google Drive (rclone remote "vpsadmin") as the vps-admin user, with
# a fixed systemd unit so the mount comes back on boot.
# Installed root-owned in /opt/vps-admin; sudoers lets the agent call it.
#
#   storage-helper mount vpsadmin    write the unit, enable and start it
#   storage-helper umount vpsadmin   stop it and remove the unit
#   storage-helper volume-export NAME  tar a Docker volume into /var/lib/vps-admin/volumes
#   storage-helper volume-import NAME  empty a Docker volume and unpack that tar into it
set -eu

action=${1:-}
name=${2:-}

target=/srv/storage/$name
conf=/var/lib/vps-admin/rclone.conf
unit=/etc/systemd/system/rclone-$name.service
work=/var/lib/vps-admin/volumes

find_fuse() {
	command -v fusermount3 2>/dev/null || command -v fusermount 2>/dev/null || true
}

# A volume name reaches tar and a path under /var/lib/docker: keep it to what Docker
# itself accepts.
valid_volume() {
	case "$1" in
	'' | [!A-Za-z0-9]* | *[!A-Za-z0-9_.-]*) return 1 ;;
	esac
	return 0
}

volume_mount() {
	valid_volume "$1" || { echo "invalid volume name" >&2; exit 64; }
	docker volume inspect "$1" >/dev/null 2>&1 ||
		{ echo "no such volume: $1" >&2; exit 66; }
	mp=$(docker volume inspect -f '{{.Mountpoint}}' "$1")
	case "$mp" in
	/*) ;;
	*) echo "docker reported no usable mountpoint" >&2; exit 69 ;;
	esac
	[ -d "$mp" ] || { echo "the volume folder is missing: $mp" >&2; exit 66; }
	printf '%s' "$mp"
}

case "$action" in
mount|umount)
	[ "$name" = vpsadmin ] || { echo "invalid storage name" >&2; exit 64; }
	;;
esac

case "$action" in
volume-export)
	mp=$(volume_mount "$name") || exit $?
	install -d -o vps-admin -g vps-admin -m 700 "$work"
	tar_file=$work/$name.tar
	rm -f "$tar_file"
	tar -C "$mp" -cf "$tar_file" --numeric-owner .
	chown vps-admin:vps-admin "$tar_file"
	chmod 600 "$tar_file"
	;;
volume-import)
	# A fresh server restores before the app ever deployed: the volume does not exist yet.
	valid_volume "$name" || { echo "invalid volume name" >&2; exit 64; }
	docker volume inspect "$name" >/dev/null 2>&1 || docker volume create "$name" >/dev/null
	mp=$(volume_mount "$name") || exit $?
	tar_file=$work/$name.tar
	[ -f "$tar_file" ] || { echo "no archive to restore for $name" >&2; exit 66; }
	find "$mp" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
	tar -C "$mp" -xf "$tar_file" --numeric-owner -p
	;;
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
	echo "usage: storage-helper mount|umount vpsadmin|volume-export NAME|volume-import NAME" >&2
	exit 64
	;;
esac
