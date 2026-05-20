#!/bin/sh
set -eu

# Caddy install + Notifycat reverse-proxy config for a single EC2 box.
# Idempotent: re-running upgrades the binary in place and rewrites the
# Caddyfile from current env. The previous Caddyfile is backed up.

usage() {
  cat <<'USAGE'
Install Caddy and configure it as an HTTPS reverse proxy for Notifycat.

Required:
  NOTIFYCAT_DOMAIN     Public DNS name pointing at this host, e.g. notifycat.example.com

Optional:
  CADDY_EMAIL          Contact email for Let's Encrypt registration (recommended)
  NOTIFYCAT_UPSTREAM   Upstream address Caddy proxies to. Default: 127.0.0.1:8080
  CADDY_VERSION        Caddy version tag without the leading "v". Default: latest release on GitHub

Examples:
  sudo NOTIFYCAT_DOMAIN=notifycat.example.com \
       CADDY_EMAIL=ops@example.com \
       ./scripts/caddy-install.sh

Run this on the EC2 instance as root (or via sudo). Open inbound 80 and 443
in the security group; Notifycat itself stays bound to 127.0.0.1:8080.
USAGE
}

fail() {
  printf 'error: %s\n\n' "$1" >&2
  usage >&2
  exit 1
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

[ "$(id -u)" -eq 0 ] || fail "this script must run as root (try: sudo $0)"
[ "${NOTIFYCAT_DOMAIN:-}" ] || fail "NOTIFYCAT_DOMAIN is required"

upstream=${NOTIFYCAT_UPSTREAM:-127.0.0.1:8080}
version=${CADDY_VERSION:-}

for cmd in curl tar systemctl; do
  command -v "$cmd" >/dev/null 2>&1 || fail "$cmd is required but was not found in PATH"
done

case "$(uname -m)" in
  x86_64)  arch=amd64 ;;
  aarch64) arch=arm64 ;;
  armv7l)  arch=armv7 ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

if [ -z "$version" ]; then
  printf 'Resolving latest Caddy release from GitHub...\n' >&2
  version=$(curl -fsSL https://api.github.com/repos/caddyserver/caddy/releases/latest \
    | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p' \
    | head -n1)
  [ "$version" ] || fail "could not resolve latest Caddy version (set CADDY_VERSION explicitly)"
fi

printf 'Installing Caddy v%s for linux/%s...\n' "$version" "$arch" >&2

# --- binary ----------------------------------------------------------------

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT INT TERM

tarball="caddy_${version}_linux_${arch}.tar.gz"
url="https://github.com/caddyserver/caddy/releases/download/v${version}/${tarball}"

curl -fsSL "$url" -o "$tmpdir/$tarball"
tar -xzf "$tmpdir/$tarball" -C "$tmpdir" caddy
install -m 0755 "$tmpdir/caddy" /usr/local/bin/caddy

# --- user, dirs ------------------------------------------------------------

if ! id caddy >/dev/null 2>&1; then
  useradd --system \
    --home /var/lib/caddy \
    --create-home \
    --shell /usr/sbin/nologin \
    --comment 'Caddy web server' \
    caddy
fi

install -d -o caddy -g caddy -m 0750 /var/lib/caddy
install -d -o caddy -g caddy -m 0755 /etc/caddy

# --- Caddyfile -------------------------------------------------------------

caddyfile=/etc/caddy/Caddyfile
if [ -f "$caddyfile" ]; then
  cp -a "$caddyfile" "${caddyfile}.bak.$(date +%Y%m%d%H%M%S)"
fi

{
  if [ "${CADDY_EMAIL:-}" ]; then
    printf '{\n\temail %s\n}\n\n' "$CADDY_EMAIL"
  fi
  cat <<CADDYFILE
$NOTIFYCAT_DOMAIN {
	reverse_proxy $upstream
	encode gzip zstd
}
CADDYFILE
} >"$caddyfile"

chown caddy:caddy "$caddyfile"
chmod 0644 "$caddyfile"

/usr/local/bin/caddy fmt --overwrite "$caddyfile" >/dev/null
/usr/local/bin/caddy validate --config "$caddyfile" >/dev/null

# --- systemd unit ----------------------------------------------------------
# Canonical unit from https://github.com/caddyserver/dist/blob/master/init/caddy.service

unit=/etc/systemd/system/caddy.service
cat >"$unit" <<'UNIT'
[Unit]
Description=Caddy
Documentation=https://caddyserver.com/docs/
After=network.target network-online.target
Requires=network-online.target

[Service]
Type=notify
User=caddy
Group=caddy
ExecStart=/usr/local/bin/caddy run --environ --config /etc/caddy/Caddyfile
ExecReload=/usr/local/bin/caddy reload --config /etc/caddy/Caddyfile --force
TimeoutStopSec=5s
LimitNOFILE=1048576
PrivateTmp=true
PrivateDevices=true
ProtectHome=true
ProtectSystem=full
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
UNIT

chmod 0644 "$unit"
systemctl daemon-reload

if systemctl is-active --quiet caddy; then
  systemctl reload caddy || systemctl restart caddy
else
  systemctl enable --now caddy
fi

# --- summary ---------------------------------------------------------------

cat <<SUMMARY

Caddy v$version installed and serving $NOTIFYCAT_DOMAIN → $upstream.

  config:   /etc/caddy/Caddyfile
  binary:   /usr/local/bin/caddy
  service:  systemctl status caddy
  logs:     journalctl -u caddy -f

First-time TLS provisioning takes a few seconds — Caddy fetches a
Let's Encrypt certificate via the HTTP-01 challenge on port 80. If
provisioning fails, check that:
  - the EC2 security group allows inbound 80 and 443 from 0.0.0.0/0
  - $NOTIFYCAT_DOMAIN resolves to this host's public IP
  - Notifycat is reachable at $upstream (curl http://$upstream/healthz)
SUMMARY
