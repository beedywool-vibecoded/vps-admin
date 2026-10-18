#!/usr/bin/env bash
# VPS Admin installer. Run as root on the server:
#
#   curl -fsSL https://github.com/beedywool/vps-admin/releases/latest/download/install.sh | sudo bash -s -- --domain admin.example.com
#
# Options:
#   --domain HOST     public hostname of the panel (needed for passkeys, served over https)
#   --version TAG     install this release instead of the latest
#   --binary PATH     install a local binary instead of downloading one (testing)
#   --no-caddy        do not touch Caddy even if it is installed
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
USE_CADDY=1
TOKEN="${VPS_ADMIN_GITHUB_TOKEN:-}"

say() { printf '\033[1;35m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
	case "$1" in
		--domain) DOMAIN="${2:-}"; shift 2 ;;
		--version) VERSION="${2:-}"; shift 2 ;;
		--binary) LOCAL_BINARY="${2:-}"; shift 2 ;;
		--no-caddy) USE_CADDY=0; shift ;;
		--token) TOKEN="${2:-}"; shift 2 ;;
		-h|--help) sed -n '2,15p' "$0"; exit 0 ;;
		*) die "unknown option $1" ;;
	esac
done

[ "$(id -u)" -eq 0 ] || die "run as root (sudo bash install.sh)"
command -v systemctl >/dev/null || die "systemd is required"
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

PUBLIC_URL="http://localhost:$PORT"
[ -n "$DOMAIN" ] && PUBLIC_URL="https://$DOMAIN"
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
	[ -n "$DOMAIN" ] && sed -i "s#\"public_url\": *\"[^\"]*\"#\"public_url\": \"$PUBLIC_URL\"#" /etc/vps-admin/config.json
	if [ -n "$TOKEN" ]; then
		if grep -q '"github_token"' /etc/vps-admin/config.json; then
			sed -i "s#\"github_token\": *\"[^\"]*\"#\"github_token\": \"$TOKEN\"#" /etc/vps-admin/config.json
		else
			sed -i "s#\"update\": *{#\"update\": { \"github_token\": \"$TOKEN\",#" /etc/vps-admin/config.json
		fi
	fi
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
allow systemctl "start *" "stop *" "restart *" "reload *"
printf '# Managed by the VPS Admin installer, rewritten on every run.\nDefaults:vps-admin !requiretty\n%s' "$rules" > "$TMP/sudoers"
visudo -cf "$TMP/sudoers" >/dev/null || die "generated sudoers file is invalid"
install -o root -g root -m 440 "$TMP/sudoers" /etc/sudoers.d/vps-admin

# 4. Service --------------------------------------------------------------------------
say "Installing vps-admin $NEW_VERSION"
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo "")
fetch_asset() { # name, destination
	# Local files only with --binary: piped through bash, the "script dir" is just the cwd.
	if [ -n "$LOCAL_BINARY" ] && [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/$1" ]; then
		cp "$SCRIPT_DIR/$1" "$2"
	elif [ -n "$TOKEN" ] && declare -F get >/dev/null; then
		get "$1" "$2"
	else
		curl -fsSL -o "$2" "${BASE:-https://github.com/$REPO/releases/latest/download}/$1"
	fi
}
fetch_asset preflight.sh "$TMP/preflight.sh"
fetch_asset vps-admin.service "$TMP/vps-admin.service"
install -o root -g root -m 755 "$TMP/preflight.sh" /opt/vps-admin/preflight.sh
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
if [ -n "$DOMAIN" ] && [ "$USE_CADDY" = 1 ] && command -v caddy >/dev/null && [ -f /etc/caddy/Caddyfile ]; then
	say "Adding $DOMAIN to Caddy"
	install -d /etc/caddy/conf.d
	cat > /etc/caddy/conf.d/vps-admin.caddy <<EOF
# Managed by the VPS Admin installer
$DOMAIN {
	encode zstd gzip
	reverse_proxy 127.0.0.1:$PORT {
		flush_interval -1
	}
}
EOF
	cp -a /etc/caddy/Caddyfile "/etc/caddy/Caddyfile.$(date +%Y%m%d%H%M%S).bak"
	grep -q '^import conf.d/\*.caddy' /etc/caddy/Caddyfile || printf '\nimport conf.d/*.caddy\n' >> /etc/caddy/Caddyfile
	if caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; then
		systemctl reload caddy
	else
		rm -f /etc/caddy/conf.d/vps-admin.caddy
		say "Caddy rejected the config, left it unchanged. Proxy $DOMAIN to 127.0.0.1:$PORT yourself."
	fi
elif [ -n "$DOMAIN" ]; then
	say "Point your reverse proxy for $DOMAIN to 127.0.0.1:$PORT (TLS is required for passkeys)."
fi

# 6. Pairing --------------------------------------------------------------------------
/opt/vps-admin/bin/vps-admin pair
echo "  New code later: sudo vps-admin pair"
echo
