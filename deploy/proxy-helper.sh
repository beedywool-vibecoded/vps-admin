#!/bin/sh
# Publishing actions the agent needs root for, with checked arguments and fixed command
# lines, so sudo can allow this helper without allowing certbot or tailscale as a whole
# (certbot hooks, for one, would run anything as root).
# Installed root-owned in /opt/vps-admin; sudoers lets the agent call it.
#
#   proxy-helper nginx-reload          test the nginx config, then reload it
#   proxy-helper certbot DOMAIN        certificate for DOMAIN, installed in its nginx site
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
	# Without an account yet, register one without email (the panel cannot ask).
	if [ -z "$(ls -A /etc/letsencrypt/accounts 2>/dev/null)" ]; then
		set -- "$@" --register-unsafely-without-email
	fi
	exec certbot "$@"
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
	echo "usage: proxy-helper nginx-reload | certbot DOMAIN | ts-serve PORT TARGET | ts-off PORT" >&2
	exit 64
	;;
esac
