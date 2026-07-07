# Manual Docker

The published image runs as a single process with one bind-mounted directory holding all state. The same image powers local testing on a dev machine and production deployments on a single VM.

!!! tip "Recommended production path: Docker Compose"
    For a production HTTPS install, use [Install with Docker Compose](compose.md) — it brings up Notifycat and Caddy together and handles TLS automatically. The `docker run` flows below are the manual alternative when you manage Caddy yourself.

## Quick reference

| | |
| --- | --- |
| Image | `ghcr.io/mptooling/notifycat:latest` (also `:<version>` / `:<major>` / `:<major>.<minor>`) |
| Binaries | `notifycat-server`, `notifycat-config`, `notifycat-migrate`, `notifycat-doctor`, `notifycat-smoke`, `notifycat-reconcile` |
| WORKDIR | `/app` — every state file lives here |
| Default `NOTIFYCAT_CONFIG_FILE` | `/app/config.yaml` |
| HTTP port | `8080` |
| Default user | `65532:65532` (override with `--user $(id -u):$(id -g)` for host-owned volumes) |
| Entrypoint | none — pass a binary name as the command; the default `CMD` is `notifycat-server` |

A single host directory mounted at `/app` is the entire surface: `config.yaml`, `config.lock`, `notifycat.db`. `.env` is passed separately via `--env-file`. Set `database.url` in `config.yaml` to `file:/app/notifycat.db` (or another path under `/app`).

## Supported tags

Each release publishes the multi-arch image under four tags: immutable `vX.Y.Z`, floating `vX.Y` and `vX`, and `latest`. Pin `vX.Y.Z` for reproducible deploys; track `latest` if you always want the newest release. All available tags are listed on the [GHCR package page](https://github.com/mptooling/notifycat/pkgs/container/notifycat).

Every open same-repository pull request also publishes a `pr-<number>` tag — rebuilt on each push, deleted when the PR closes — for smoke-testing a change on a real server before it ships. Never point production at a `pr-*` tag.

### Verifying the install bundle

Every release attaches the install files — `compose.yaml`, `Caddyfile`, the `notifycat` wrapper, `env.example`, `config.example.yaml`, `install.sh` — plus a `SHA256SUMS` manifest. (The env template is `env.example` because GitHub rewrites asset names starting with a dot; `install.sh` saves it as `.env.example`.) The installer verifies automatically; to check a manual download, fetch the assets and manifest into one directory and run:

```sh
sha256sum -c SHA256SUMS      # macOS/BSD: shasum -a 256 -c SHA256SUMS
```

## Local Docker run

Before you start: create and install the [Slack app](slack-app.md) — `.env` needs its bot token — and [generate a webhook secret](security.md#generating-the-webhook-secret). The webhook itself is registered last, once the server is up and reachable.

Try Notifycat against real Slack and GitHub without installing Go — five commands plus a tunnel:

```sh
mkdir -p ~/notifycat && cd ~/notifycat

# 1. Pull the config templates
curl -fsSL https://github.com/mptooling/notifycat/releases/latest/download/env.example          -o .env
curl -fsSL https://github.com/mptooling/notifycat/releases/latest/download/config.example.yaml  -o config.yaml

# 2. Edit them
$EDITOR .env           # set the webhook secret and SLACK_BOT_TOKEN
$EDITOR config.yaml    # set database.url and point repositories at real Slack channel IDs

# 3. Validate the mappings against Slack (and the git host, if a read token is set)
docker run --rm --user $(id -u):$(id -g) -v "$PWD:/app" --env-file .env \
  ghcr.io/mptooling/notifycat:latest notifycat-config validate

# 4. Preflight
docker run --rm --user $(id -u):$(id -g) -v "$PWD:/app" --env-file .env \
  ghcr.io/mptooling/notifycat:latest notifycat-doctor

# 5. Start the server (detached, restart on crash, host-only port)
docker run -d --name notifycat --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  --user $(id -u):$(id -g) -v "$PWD:/app" --env-file .env \
  ghcr.io/mptooling/notifycat:latest
```

Check health:

```sh
curl -i http://localhost:8080/healthz   # expect 200 OK
docker logs -f notifycat
```

`--user $(id -u):$(id -g)` runs the container as your host user, so the bind-mounted directory is naturally writable — no `chown` needed. Omit the flag and the image default UID `65532` applies (fine when an orchestrator prepares the volumes).

### Exposing the local server

The git host needs a public HTTPS URL. For local testing, run a tunnel and register the webhook against its URL:

```sh
ngrok http 8080
# or
cloudflared tunnel --url http://localhost:8080
```

Then follow [GitHub webhook setup](github-webhook.md) or [Bitbucket webhook setup](bitbucket-webhook.md).

## Production deploy on a single VM (manual Caddy, EC2 example)

!!! note
    [Docker Compose](compose.md) is the recommended production path. Use this section only when Caddy must run as a host service rather than a container.

End-state: the VM runs Notifycat in Docker and Caddy on the host for TLS termination and certificate auto-renewal, proxying `https://notifycat.example.com` → `http://127.0.0.1:8080`.

### Prerequisites

| | |
| --- | --- |
| Instance | t3.micro or larger; Ubuntu 22.04+ or Amazon Linux 2023 |
| Public IP | static (Elastic IP), or stable enough that DNS won't drift |
| DNS A record | `notifycat.example.com` → the instance's public IP |
| Inbound rules | 22 (SSH from your IP), 80 (HTTP-01 challenge + redirect), 443 (HTTPS) |
| Docker | installed; your user in the `docker` group |

```sh
sudo apt-get update && sudo apt-get install -y docker.io
sudo usermod -aG docker "$USER"   # log out and back in
```

### Deploy

Steps 1–4 are identical to the [local run](#local-docker-run) above — templates, edit, validate, preflight, `docker run`. Then add TLS:

```sh
# TLS + reverse proxy (clones the repository to get the script — or scp it over yourself)
git clone https://github.com/mptooling/notifycat.git /tmp/notifycat
sudo NOTIFYCAT_DOMAIN=notifycat.example.com \
     CADDY_EMAIL=ops@example.com \
     /tmp/notifycat/scripts/caddy-install.sh
```

Then hit `https://notifycat.example.com/healthz` — you should see `200 OK` with a valid Let's Encrypt certificate.

### What `caddy-install.sh` does

A single idempotent POSIX-sh script — safe to re-run to upgrade Caddy or regenerate the Caddyfile from new env values:

1. Downloads the latest Caddy release binary (override with `CADDY_VERSION`) for `amd64`/`arm64`/`armv7` into `/usr/local/bin/caddy`.
2. Creates the `caddy` system user, `/etc/caddy/`, `/var/lib/caddy/`.
3. Writes `/etc/caddy/Caddyfile` (backing up any existing one):

   ```caddyfile
   {
       email <CADDY_EMAIL>
   }

   <NOTIFYCAT_DOMAIN> {
       reverse_proxy 127.0.0.1:8080
       encode gzip zstd
   }
   ```

4. Installs the canonical hardened systemd unit (`PrivateTmp`, `ProtectSystem=full`, `ProtectHome`, `CAP_NET_BIND_SERVICE` so the unprivileged user can bind 80/443).
5. Runs `caddy fmt` + `caddy validate` before activating.
6. `systemctl enable --now caddy` on first run; `systemctl reload caddy` afterwards.

Certificate auto-renewal is automatic and in-process — no cron, no certbot. Renewals log to `journalctl -u caddy`.

### Verification

| Check | Command | Expected |
| --- | --- | --- |
| Caddy is running | `systemctl status caddy` | `active (running)` |
| Cert provisioned | `journalctl -u caddy --grep="certificate obtained successfully"` | one line per (sub)domain |
| Reachable through Caddy | `curl -i https://notifycat.example.com/healthz` | `HTTP/2 200` |
| Upstream reachable on the box | `curl -i http://127.0.0.1:8080/healthz` | `HTTP/1.1 200 OK` |
| Survives reboot | `sudo reboot`, then `docker ps` | `notifycat` running, Caddy active |

ACME/certificate errors are covered in [Troubleshooting → Certificate failures](troubleshooting.md#certificate-failures); container and volume problems in [Database issues](troubleshooting.md#database-issues) and [Server exits at startup](troubleshooting.md#server-exits-at-startup).

## Building the image locally

```sh
docker build -t notifycat:dev .
```

The justfile has `just docker-build`, `just docker-validate`, `just docker-doctor`, `just docker-up` for the same flows against the local build.
