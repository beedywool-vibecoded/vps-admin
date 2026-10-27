#!/bin/sh
# Publishing actions that need root, with checked arguments and fixed command lines so
# sudo doesn't have to allow certbot or tailscale as a whole (certbot hooks run anything).
# Installed root-owned in /opt/vps-admin; sudoers lets the agent call it.
#
#   proxy-helper nginx-reload          test the nginx config, then reload it
#   proxy-helper certbot DOMAIN        certificate for DOMAIN, installed in its nginx site
#   proxy-helper caddy-write FILE      replace a root-owned Caddy file with the staged one
#   proxy-helper ts-serve PORT TARGET  https://<tailscale name>:PORT -> 127.0.0.1:TARGET
#   proxy-helper ts-off PORT           stop serving PORT on the tailnet
set -eu
action=${1:-}

port() {
	case "$1" in '' | *[!0-9]*) echo "invalid port: $1" >&2; exit 64 ;; esac
	[ "$1" -ge 1 ] && [ "$1" -le 65535 ] || { echo "invalid port: $1" >&2; exit 64; }
}

case "$action" in
nginx-reload)
	nginx -t
	exec systemctl reload nginx
	;;
certbot)
	domain=${2:-}
	printf '%s' "$domain" | grep -Eq '^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$' ||
		{ echo "invalid domain: $domain" >&2; exit 64; }
	set -- --nginx -d "$domain" --non-interactive --agree-tos --keep-until-expiring --redirect
	# Register an account without email on first use (the panel can't ask).
	if [ -z "$(ls -A /etc/letsencrypt/accounts 2>/dev/null)" ]; then
		set -- "$@" --register-unsafely-without-email
	fi
	exec certbot "$@"
	;;
caddy-write)
	target=${2:-}
	case "$target" in
	/etc/caddy/Caddyfile) ;;
	/etc/caddy/*.caddy) ;;
	*) echo "invalid Caddy file: $target" >&2; exit 64 ;;
	esac
	case "$target" in *..*) echo "invalid Caddy file: $target" >&2; exit 64 ;; esac
	command -v caddy >/dev/null 2>&1 || { echo "caddy is not installed" >&2; exit 69; }
	stage=/var/lib/vps-admin/caddy-edit.new
	[ -f "$stage" ] || { echo "no staged Caddy configuration" >&2; exit 66; }
	[ -f "$target" ] || { echo "no such Caddy file: $target" >&2; exit 66; }
	# Same directory so the swap is an atomic rename.
	tmp=$(mktemp "/etc/caddy/.vps-admin.XXXXXX") || exit 69
	trap 'rm -f "$tmp"' EXIT
	cp "$stage" "$tmp"
	chown root:root "$tmp"
	chmod 644 "$tmp"
	backup=/var/lib/vps-admin/caddy-edit.prev
	cp -a "$target" "$backup"
	mv -f "$tmp" "$target"
	if ! caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; then
		cp -a "$backup" "$target"
		rm -f "$backup"
		echo "Caddy refused this configuration, the previous file was put back" >&2
		exit 65
	fi
	rm -f "$backup"
	;;
ts-serve)
	port "${2:-}"
	port "${3:-}"
	exec tailscale serve --bg --https="$2" "http://127.0.0.1:$3"
	;;
ts-off)
	port "${2:-}"
	exec tailscale serve --https="$2" off
	;;
*)
	echo "usage: proxy-helper nginx-reload | certbot DOMAIN | caddy-write FILE | ts-serve PORT TARGET | ts-off PORT" >&2
	exit 64
	;;
esac
