#!/usr/bin/env bash
# VPS Admin installer. Run as root on the server:
#
#   curl -fsSL https://github.com/beedywool/vps-admin/releases/latest/download/install.sh | sudo bash -s -- --domain admin.example.com
#
# Options:
#   --domain HOST     public hostname of the panel (needed for passkeys, served over https)
#   --version TAG     install this release instead of the latest
#   --binary PATH     install a local binary instead of downloading one (testing)
#   --proxy NAME      reverse proxy to configure: auto (default: Caddy, else nginx), caddy,
#                     nginx or none
#   --no-caddy        same as --proxy none (kept for older commands)
#   --email ADDRESS   for Let's Encrypt with nginx (certbot); asked when needed otherwise
#   --no-tls          with nginx, do not request a certificate
#   --tailscale       serve the panel only on this machine's Tailscale name
#                     (https://NAME.TAILNET.ts.net, tailscale serve); offered when Tailscale
#                     runs. The --domain then only receives GitHub webhooks.
#   --public          keep the panel public even if Tailscale runs (no question)
#   --allow CIDRS     comma separated networks allowed to reach the panel through the
#                     proxy (a WireGuard subnet, an office IP); others get 403. GitHub
#                     webhooks and certificate renewals stay reachable.
#   --token TOKEN     GitHub token for a private repository (read-only Contents access);
#                     also saved in the config so self-update keeps working
#
# Running it again repairs or upgrades an existing install and keeps its data.
set -euo pipefail

REPO="beedywool/vps-admin"
# Same key as internal/update/pubkey.go (CI checks they match).
PUBLIC_KEY="0HOlgqmN83uAfkoHv/n1QkQFF/gAvBSbPcRJlj/7P44="
PORT=7171
DOMAIN=""
VERSION=""
LOCAL_BINARY=""
PROXY=auto
EMAIL=""
TLS=1
TAILSCALE=ask
ALLOW=""
TS_NAME=""
TS_PORT=443
TOKEN="${VPS_ADMIN_GITHUB_TOKEN:-}"

say() { printf '\033[1;35m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }
# Questions go to the terminal even when the script is piped into bash.
ask() {
	local reply=""
	# No terminal (cron, CI, a pipe without tty): no question, the caller's default.
	{ printf '%s ' "$1" >/dev/tty; } 2>/dev/null || return 1
	{ read -r reply </dev/tty; } 2>/dev/null || return 1
	printf '%s' "$reply"
}

while [ $# -gt 0 ]; do
	case "$1" in
		--domain) DOMAIN="${2:-}"; shift 2 ;;
		--version) VERSION="${2:-}"; shift 2 ;;
		--binary) LOCAL_BINARY="${2:-}"; shift 2 ;;
		--proxy) PROXY="${2:-}"; shift 2 ;;
		--no-caddy) PROXY=none; shift ;;
		--email) EMAIL="${2:-}"; shift 2 ;;
		--no-tls) TLS=0; shift ;;
		--tailscale) TAILSCALE=yes; shift ;;
		--public) TAILSCALE=no; shift ;;
		--allow) ALLOW="${2:-}"; shift 2 ;;
		--token) TOKEN="${2:-}"; shift 2 ;;
		-h|--help) sed -n '2,26p' "$0"; exit 0 ;;
		*) die "unknown option $1" ;;
	esac
done

[ "$(id -u)" -eq 0 ] || die "run as root (sudo bash install.sh)"
command -v systemctl >/dev/null || die "systemd is required"
case "$PROXY" in auto|caddy|nginx|none) ;; *) die "--proxy takes auto, caddy, nginx or none" ;; esac
if [ -n "$ALLOW" ] && ! [[ "$ALLOW" =~ ^[0-9a-fA-F:./,]+$ ]]; then
	die "--allow takes networks like 10.8.0.0/24,192.168.1.0/24"
fi
if [ -n "$DOMAIN" ] && ! [[ "$DOMAIN" =~ ^[A-Za-z0-9.-]+$ ]]; then
	die "--domain takes a hostname, like admin.example.com"
fi

case "$(uname -m)" in
	x86_64|amd64) ARCH=amd64 ;;
	aarch64|arm64) ARCH=arm64 ;;
	*) die "unsupported architecture $(uname -m)" ;;
esac

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# 1. Binary ---------------------------------------------------------------------------
if [ -n "$LOCAL_BINARY" ]; then
	say "Using local binary $LOCAL_BINARY"
	cp "$LOCAL_BINARY" "$TMP/vps-admin"
else
	command -v curl >/dev/null || die "curl is required"
	command -v openssl >/dev/null || die "openssl is required to verify the release signature"
	if [ -n "$VERSION" ]; then
		BASE="https://github.com/$REPO/releases/download/$VERSION"
	else
		BASE="https://github.com/$REPO/releases/latest/download"
	fi
	say "Downloading vps-admin for linux/$ARCH"
	if [ -n "$TOKEN" ]; then
		# Private repository: download links need the API and the token.
		api="https://api.github.com/repos/$REPO/releases/latest"
		[ -n "$VERSION" ] && api="https://api.github.com/repos/$REPO/releases/tags/$VERSION"
		curl -fsSL -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.github+json" -o "$TMP/release.json" "$api" \
			|| die "cannot read the release (check the token and its access to $REPO)"
		get() { # asset name, destination
			local url
			# Each asset object lists its API "url" before its "name"; no jq needed.
			url=$(awk -v want="$1" '
				/"url": *"https:\/\/api\.github\.com\/repos\/.*\/releases\/assets\// { match($0, /https:[^"]*/); u = substr($0, RSTART, RLENGTH) }
				/"name": *"/ { if (index($0, "\"" want "\"") && u != "") { print u; exit } }' "$TMP/release.json")
			[ -n "$url" ] || die "the release has no $1"
			curl -fsSL -H "Authorization: Bearer $TOKEN" -H "Accept: application/octet-stream" -o "$2" "$url"
		}
	else
		get() { curl -fsSL -o "$2" "$BASE/$1"; }
	fi
	get "vps-admin_linux_$ARCH" "$TMP/vps-admin"
	get SHA256SUMS "$TMP/SHA256SUMS"
	get SHA256SUMS.sig "$TMP/SHA256SUMS.sig"

	say "Verifying the signature"
	# Wrap the raw ed25519 key in its DER header so openssl can read it.
	{ printf '\x30\x2a\x30\x05\x06\x03\x2b\x65\x70\x03\x21\x00'; printf '%s' "$PUBLIC_KEY" | base64 -d; } > "$TMP/pub.der"
	base64 -d "$TMP/SHA256SUMS.sig" > "$TMP/sig.bin"
	openssl pkeyutl -verify -pubin -keyform DER -inkey "$TMP/pub.der" -rawin \
		-in "$TMP/SHA256SUMS" -sigfile "$TMP/sig.bin" >/dev/null 2>&1 \
		|| die "signature check failed (OpenSSL 3 is needed); refusing to install"
	(cd "$TMP" && grep " vps-admin_linux_$ARCH\$" SHA256SUMS | sed "s/vps-admin_linux_$ARCH/vps-admin/" | sha256sum -c --quiet) \
		|| die "checksum mismatch; refusing to install"
fi
chmod 755 "$TMP/vps-admin"
NEW_VERSION=$("$TMP/vps-admin" version) || die "the binary does not run on this machine"

# 2. User and directories -------------------------------------------------------------
if ! id vps-admin >/dev/null 2>&1; then
	say "Creating the vps-admin system user"
	useradd --system --home-dir /var/lib/vps-admin --shell /usr/sbin/nologin vps-admin
fi
# docker: manage containers (this is root-equivalent, like any Docker admin tool).
# systemd-journal and adm: read service logs and auth.log without sudo.
for g in docker systemd-journal adm; do
	getent group "$g" >/dev/null && usermod -aG "$g" vps-admin
done

install -d -o vps-admin -g vps-admin -m 755 /opt/vps-admin/bin
install -d -o vps-admin -g vps-admin -m 700 /var/lib/vps-admin
install -d -o root -g vps-admin -m 750 /etc/vps-admin
# Scripts linked with `sudo vps-admin link`: root-owned, so the agent cannot add any.
install -d -o root -g root -m 755 /etc/vps-admin/scripts

# Private panel through Tailscale: its MagicDNS name with a certificate from the tailnet.
if [ "$TAILSCALE" != no ] && command -v tailscale >/dev/null && tailscale status >/dev/null 2>&1; then
	TS_NAME=$(tailscale status --json 2>/dev/null | sed -n 's/^ *"DNSName": *"\([^"]*\)\.",*$/\1/p' | head -1)
	if [ "$TAILSCALE" = ask ] && [ -n "$TS_NAME" ]; then
		reply=$(ask "Tailscale runs here. Make the panel reachable only through it, at https://$TS_NAME? [y/N]" || echo n)
		[ "$reply" = y ] || [ "$reply" = Y ] && TAILSCALE=yes
	fi
fi
if [ "$TAILSCALE" = yes ]; then
	[ -n "$TS_NAME" ] || die "--tailscale: Tailscale is not running here (tailscale up first)"
	# The JSON spreads the list over lines: join them before looking.
	if ! tailscale status --json 2>/dev/null | tr -d ' \n\t' | grep -q '"CertDomains":\["'; then
		die "--tailscale needs HTTPS certificates in your tailnet: Tailscale admin console, DNS, enable HTTPS. Then rerun."
	fi
fi

PUBLIC_URL="http://localhost:$PORT"
[ -n "$DOMAIN" ] && PUBLIC_URL="https://$DOMAIN"
WEBHOOK_URL=""
if [ "$TAILSCALE" = yes ]; then
	# Port 443 on the tailnet address would block a local proxy that listens on [::]:443
	# (nginx and Caddy do): then the panel takes 10443 on the tailnet instead.
	if ss -Hltnp 2>/dev/null | grep -v tailscaled | awk '{print $4}' | grep -qE '(^|[]:*])443$'; then
		TS_PORT=10443
	fi
	PUBLIC_URL="https://$TS_NAME"
	[ "$TS_PORT" = 443 ] || PUBLIC_URL="https://$TS_NAME:$TS_PORT"
	[ -n "$DOMAIN" ] && WEBHOOK_URL="https://$DOMAIN"
fi
if [ ! -f /etc/vps-admin/config.json ]; then
	cat > /etc/vps-admin/config.json <<EOF
{
  "listen": "127.0.0.1:$PORT",
  "public_url": "$PUBLIC_URL",
  "data_dir": "/var/lib/vps-admin",
  "trusted_proxies": ["127.0.0.1/32", "::1/128"],
  "docker_socket": "/var/run/docker.sock",
  "update": { "auto": true, "prerelease": false, "github_token": "$TOKEN" }
}
EOF
else
	{ [ -n "$DOMAIN" ] || [ "$TAILSCALE" = yes ]; } && sed -i "s#\"public_url\": *\"[^\"]*\"#\"public_url\": \"$PUBLIC_URL\"#" /etc/vps-admin/config.json
	if [ -n "$TOKEN" ]; then
		if grep -q '"github_token"' /etc/vps-admin/config.json; then
			sed -i "s#\"github_token\": *\"[^\"]*\"#\"github_token\": \"$TOKEN\"#" /etc/vps-admin/config.json
		else
			sed -i "s#\"update\": *{#\"update\": { \"github_token\": \"$TOKEN\",#" /etc/vps-admin/config.json
		fi
	fi
fi
# GitHub pushes reach a private panel through the public domain.
sed -i '/"webhook_url"/d' /etc/vps-admin/config.json
if [ -n "$WEBHOOK_URL" ]; then
	sed -i "s#\"public_url\": *\"[^\"]*\",#&\n  \"webhook_url\": \"$WEBHOOK_URL\",#" /etc/vps-admin/config.json
fi
chown root:vps-admin /etc/vps-admin/config.json
chmod 640 /etc/vps-admin/config.json

# 3. Privileged commands --------------------------------------------------------------
# The agent may run exactly these as root. Paths differ between distributions, so they
# are resolved here.
say "Writing the sudo allowlist"
rules=""
allow() {
	local path
	path=$(command -v "$1" 2>/dev/null) || return 0
	shift
	for args in "$@"; do
		rules+="vps-admin ALL=(root) NOPASSWD: $path $args"$'\n'
	done
}
allow sshd "-T"
allow ufw "status" "status verbose"
allow nft "list ruleset"
allow fail2ban-client "status" "status *"
allow ss "-tulnpH"
allow systemctl "start *" "stop *" "restart *" "reload *" "reboot"
# Linked scripts run as their owner through this root-owned helper.
rules+="vps-admin ALL=(root) NOPASSWD: /opt/vps-admin/run-script *"$'\n'
# Publishing through nginx, certbot or tailscale serve, with checked arguments.
rules+="vps-admin ALL=(root) NOPASSWD: /opt/vps-admin/proxy-helper *"$'\n'
printf '# Managed by the VPS Admin installer, rewritten on every run.\nDefaults:vps-admin !requiretty\n%s' "$rules" > "$TMP/sudoers"
visudo -cf "$TMP/sudoers" >/dev/null || die "generated sudoers file is invalid"
install -o root -g root -m 440 "$TMP/sudoers" /etc/sudoers.d/vps-admin

# 4. Service --------------------------------------------------------------------------
say "Installing vps-admin $NEW_VERSION"
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo "")
# The service files ship as one archive, so the release page lists one asset instead of
# four. Local files win with --binary: piped through bash, the "script dir" is the cwd.
if [ -n "$LOCAL_BINARY" ] && [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/preflight.sh" ]; then
	for f in preflight.sh run-script.sh proxy-helper.sh vps-admin.service; do
		cp "$SCRIPT_DIR/$f" "$TMP/$f"
	done
else
	archive=vps-admin-deploy.tar.gz
	if [ -n "$TOKEN" ] && declare -F get >/dev/null; then
		get "$archive" "$TMP/$archive"
	else
		curl -fsSL -o "$TMP/$archive" "${BASE:-https://github.com/$REPO/releases/latest/download}/$archive"
	fi
	tar -xzf "$TMP/$archive" -C "$TMP" || die "cannot unpack the service files"
fi
install -o root -g root -m 755 "$TMP/preflight.sh" /opt/vps-admin/preflight.sh
install -o root -g root -m 755 "$TMP/run-script.sh" /opt/vps-admin/run-script
install -o root -g root -m 755 "$TMP/proxy-helper.sh" /opt/vps-admin/proxy-helper
install -o root -g root -m 644 "$TMP/vps-admin.service" /etc/systemd/system/vps-admin.service

systemctl stop vps-admin 2>/dev/null || true
install -o vps-admin -g vps-admin -m 755 "$TMP/vps-admin" /opt/vps-admin/bin/vps-admin
rm -f /opt/vps-admin/bin/vps-admin.prev /var/lib/vps-admin/update-pending
ln -sf /opt/vps-admin/bin/vps-admin /usr/local/bin/vps-admin
systemctl daemon-reload
systemctl enable --now vps-admin >/dev/null

for _ in $(seq 1 20); do
	curl -fsS "http://127.0.0.1:$PORT/api/health" >/dev/null 2>&1 && break
	sleep 0.5
done
curl -fsS "http://127.0.0.1:$PORT/api/health" >/dev/null 2>&1 || die "the agent did not start, see: journalctl -u vps-admin"

# 5. Reverse proxy --------------------------------------------------------------------

if [ "$PROXY" = auto ]; then
	if command -v caddy >/dev/null && [ -f /etc/caddy/Caddyfile ]; then
		PROXY=caddy
	elif command -v nginx >/dev/null && [ -d /etc/nginx ]; then
		PROXY=nginx
	else
		PROXY=none
	fi
fi

setup_caddy() {
	say "Configuring Caddy"
	backup="/etc/caddy/Caddyfile.$(date +%Y%m%d%H%M%S).bak"
	cp -a /etc/caddy/Caddyfile "$backup"
	install -d /etc/caddy/conf.d
	# Deployed apps get one snippet each in this folder, written by the agent.
	install -d -o vps-admin -g vps-admin -m 755 /etc/caddy/vps-admin-apps
	if [ -n "$DOMAIN" ]; then
		if [ "$TAILSCALE" = yes ]; then
			# The panel lives on Tailscale; the domain only takes GitHub webhooks.
			cat > /etc/caddy/conf.d/vps-admin.caddy <<CADDY
# Managed by the VPS Admin installer
$DOMAIN {
	handle /api/github/webhook {
		reverse_proxy 127.0.0.1:$PORT
	}
	handle {
		respond 404
	}
}
CADDY
		else
			{
				echo "# Managed by the VPS Admin installer"
				echo "$DOMAIN {"
				echo "	encode zstd gzip"
				if [ -n "$ALLOW" ]; then
					echo "	@outside {"
					echo "		not remote_ip ${ALLOW//,/ }"
					echo "		not path /api/github/webhook"
					echo "	}"
					echo "	respond @outside 403"
				fi
				echo "	reverse_proxy 127.0.0.1:$PORT {"
				echo "		flush_interval -1"
				echo "	}"
				echo "}"
			} > /etc/caddy/conf.d/vps-admin.caddy
		fi
		grep -q '^import conf.d/\*.caddy' /etc/caddy/Caddyfile || printf '\nimport conf.d/*.caddy\n' >> /etc/caddy/Caddyfile
	fi
	grep -q '^import vps-admin-apps/\*.caddy' /etc/caddy/Caddyfile || printf '\nimport vps-admin-apps/*.caddy\n' >> /etc/caddy/Caddyfile
	if caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; then
		systemctl reload caddy
	else
		cp -a "$backup" /etc/caddy/Caddyfile
		rm -f /etc/caddy/conf.d/vps-admin.caddy
		say "Caddy rejected the new config, restored it. Proxy ${DOMAIN:-the panel} to 127.0.0.1:$PORT yourself."
	fi
}

# nginx: one site file for the panel, then a Let's Encrypt certificate through certbot,
# which adds the HTTPS part to that same file and keeps it renewed.
setup_nginx() {
	# Deployed apps get one server block each in this folder, written by the agent.
	install -d -o vps-admin -g vps-admin -m 755 /etc/nginx/vps-admin-apps
	if [ -d /etc/nginx/conf.d ] && [ ! -f /etc/nginx/conf.d/vps-admin-apps.conf ]; then
		echo "include /etc/nginx/vps-admin-apps/*.conf;" > /etc/nginx/conf.d/vps-admin-apps.conf
	fi
	[ -n "$DOMAIN" ] || { say "nginx found; pass --domain to serve the panel through it."; return; }
	say "Configuring nginx"
	local site conf link=""
	if [ -d /etc/nginx/sites-available ] && [ -d /etc/nginx/sites-enabled ]; then
		site=/etc/nginx/sites-available/vps-admin
		link=/etc/nginx/sites-enabled/vps-admin
	else
		site=/etc/nginx/conf.d/vps-admin.conf
	fi
	# Another server block for the same name would conflict with ours.
	conf=$(grep -rlE "server_name[^;]*[[:space:]]$DOMAIN([[:space:];]|$)" /etc/nginx 2>/dev/null | grep -v -e "^$site\$" -e "^$link\$" | head -1 || true)
	if [ -n "$conf" ]; then
		say "$conf already has a server block for $DOMAIN: remove it (or rerun with --proxy none) so the installer can manage it."
		return
	fi
	# Rewritten on every run (certbot puts its HTTPS lines back below, keeping the
	# certificate), so switching between public, --allow and --tailscale is a rerun.
	local proxy allow="" panel
	proxy="		proxy_pass http://127.0.0.1:$PORT;
		proxy_http_version 1.1;
		proxy_set_header Host \$host;
		proxy_set_header X-Real-IP \$remote_addr;
		proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
		proxy_set_header X-Forwarded-Proto \$scheme;
		# Live views (metrics, logs, builds, the assistant) stream: no buffering.
		proxy_buffering off;
		proxy_cache off;
		proxy_read_timeout 1h;"
	if [ -n "$ALLOW" ]; then
		for net in ${ALLOW//,/ }; do allow+="		allow $net;
"; done
		allow+="		deny all;
"
	fi
	if [ "$TAILSCALE" = yes ]; then
		panel="		return 404;"
	else
		panel="$allow$proxy"
	fi
	cat > "$site" <<NGINX
# Managed by the VPS Admin installer
server {
	listen 80;
	listen [::]:80;
	server_name $DOMAIN;
	client_max_body_size 16m;

	# GitHub pushes, always public (signed by GitHub, checked by the agent).
	location = /api/github/webhook {
$proxy
	}
	location / {
$panel
	}
}
NGINX
	[ -n "$link" ] && ln -sf "$site" "$link"
	if ! nginx -t >/dev/null 2>&1; then
		rm -f "$site" ${link:+"$link"}
		say "nginx rejected the new site, removed it. Proxy $DOMAIN to 127.0.0.1:$PORT yourself."
		return
	fi
	systemctl reload nginx

	[ "$TLS" = 1 ] || { say "No certificate requested (--no-tls): passkeys need HTTPS."; return; }
	if ! command -v certbot >/dev/null || ! certbot plugins 2>/dev/null | grep -q nginx; then
		if command -v apt-get >/dev/null && [ "$(ask "HTTPS needs certbot. Install certbot and its nginx plugin now? [Y/n]" || echo n)" != n ]; then
			apt-get install -y certbot python3-certbot-nginx >/dev/null
		else
			say "Install certbot (certbot python3-certbot-nginx), then run: certbot --nginx -d $DOMAIN"
			return
		fi
	fi
	# An existing certificate is reinstalled as is, a new one requested otherwise.
	local args=(--nginx -d "$DOMAIN" --non-interactive --agree-tos --redirect --keep-until-expiring)
	if [ -n "$EMAIL" ]; then
		args+=(-m "$EMAIL")
	elif [ -z "$(ls -A /etc/letsencrypt/accounts 2>/dev/null)" ]; then
		EMAIL=$(ask "Email for Let's Encrypt (expiry notices), empty for none:" || true)
		if [ -n "$EMAIL" ]; then args+=(-m "$EMAIL"); else args+=(--register-unsafely-without-email); fi
	fi
	say "Setting up HTTPS for $DOMAIN"
	if certbot "${args[@]}" >/dev/null 2>&1; then
		systemctl reload nginx
	else
		say "certbot failed (DNS of $DOMAIN must point here, port 80 open). Retry with: certbot --nginx -d $DOMAIN"
	fi
}

if [ "$TAILSCALE" = yes ]; then
	say "Serving the panel on $PUBLIC_URL (Tailscale only)"
	# A previous run may have served it on the other port.
	for p in 443 10443; do
		[ "$p" = "$TS_PORT" ] && continue
		tailscale serve status 2>/dev/null | grep -q "127.0.0.1:$PORT" && tailscale serve --https="$p" off >/dev/null 2>&1 || true
	done
	tailscale serve --bg --https="$TS_PORT" "http://127.0.0.1:$PORT" >/dev/null || die "tailscale serve failed, see: tailscale serve status"
elif command -v tailscale >/dev/null && tailscale serve status 2>/dev/null | grep -q "127.0.0.1:$PORT"; then
	# Back to public: stop the private address a previous run set up.
	tailscale serve --https=443 off >/dev/null 2>&1 || true
	tailscale serve --https=10443 off >/dev/null 2>&1 || true
fi

case "$PROXY" in
	caddy) setup_caddy ;;
	nginx)
		setup_nginx
		;;
	none) [ -n "$DOMAIN" ] && [ "$TAILSCALE" != yes ] && say "Point your reverse proxy for $DOMAIN to 127.0.0.1:$PORT (TLS is required for passkeys, disable response buffering)." ;;
esac

# 6. Pairing --------------------------------------------------------------------------
/opt/vps-admin/bin/vps-admin pair
echo "  New code later: sudo vps-admin pair"
echo
