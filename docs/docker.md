# Docker

The published image runs as a single process with one bind-mounted
directory holding all state. The same image powers local Docker
testing on a dev machine and production HTTPS deployments on a single
VM (EC2, Hetzner, DO droplet — anything systemd-based).

## Quick reference

| | |
| --- | --- |
| Image | `ghcr.io/mptooling/notifycat:latest` (also `:<version>` / `:<major>` / `:<major>.<minor>`) |
| Binaries | `notifycat-server`, `notifycat-mapping`, `notifycat-migrate`, `notifycat-doctor` |
| WORKDIR | `/app` — every state file lives here |
| Default `DATABASE_URL` | `file:/app/notifycat.db` |
| Default `NOTIFYCAT_MAPPINGS_FILE` | `/app/mappings.yaml` |
| HTTP port | `8080` |
| Default user | `65532:65532` (override with `--user $(id -u):$(id -g)` for host-owned volumes) |
| Entrypoint | none — pass the binary name as the command (`notifycat-doctor`, `notifycat-mapping validate`, …); the default `CMD` is `notifycat-server` |

A single host directory mounted at `/app` is the entire surface. It
holds `mappings.yaml`, `mappings.lock`, `notifycat.db`. `.env` is
passed separately via `--env-file`.

## Local Docker run

Use this when you want to try Notifycat against real GitHub + Slack
without installing Go. Five commands plus a tunnel for the GitHub
side.

```sh
mkdir -p ~/notifycat && cd ~/notifycat

# 1. Pull the config templates
curl -fsSL https://raw.githubusercontent.com/mptooling/notifycat/main/.env.example     -o .env
curl -fsSL https://raw.githubusercontent.com/mptooling/notifycat/main/mappings.example.yaml -o mappings.yaml

# 2. Edit them
$EDITOR .env            # set GITHUB_WEBHOOK_SECRET and SLACK_BOT_TOKEN
$EDITOR mappings.yaml   # point your repos at real Slack channel IDs

# 3. Validate the mappings against Slack (and GitHub, if GITHUB_TOKEN is set)
docker run --rm --user $(id -u):$(id -g) -v "$PWD:/app" --env-file .env \
  ghcr.io/mptooling/notifycat:latest notifycat-mapping validate

# 4. Preflight check (config + database + mappings; add owner/repo for per-repo Slack/GitHub probes)
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

`--user $(id -u):$(id -g)` runs the container as your host user, so
the bind-mounted `~/notifycat/` is naturally writable — no `chown`
needed. The image's default UID `65532` still applies if you omit
the flag (useful for orchestrators that set up volumes themselves).

### Exposing the local server to GitHub

GitHub needs a public HTTPS URL. For local testing, run a tunnel:

```sh
# ngrok
ngrok http 8080

# Cloudflare Tunnel (cloudflared)
cloudflared tunnel --url http://localhost:8080
```

Then register the webhook against the tunnel's HTTPS URL using
`scripts/github-webhook-create.sh` — see
[GitHub webhook setup](github-webhook.md).

## Production deploy on a single VM (EC2 example)

End-state: the EC2 box runs Notifycat in Docker and Caddy on the host
for TLS termination + Let's Encrypt cert auto-renewal. Caddy proxies
`https://notifycat.example.com` to `http://127.0.0.1:8080`.

### Prerequisites

| | |
| --- | --- |
| EC2 instance | t3.micro or larger; Ubuntu 22.04+ or Amazon Linux 2023 |
| Public IP | static (Elastic IP) or stable enough that DNS won't drift |
| DNS A record | `notifycat.example.com` → the instance's public IP |
| Security group inbound | 22 (SSH from your IP), 80 (Let's Encrypt HTTP-01 challenge + redirect), 443 (HTTPS) |
| Docker | installed on the host; the `ubuntu` (or equivalent) user in the `docker` group |

```sh
# One-time host setup on Ubuntu
sudo apt-get update && sudo apt-get install -y docker.io
sudo usermod -aG docker "$USER"   # log out + back in for the group to apply
```

### Deploy

The five-command shape is identical to local; only the hostname and
the post-step Caddy install differ.

```sh
mkdir -p ~/notifycat && cd ~/notifycat

# 1. config templates
curl -fsSL https://raw.githubusercontent.com/mptooling/notifycat/main/.env.example     -o .env
curl -fsSL https://raw.githubusercontent.com/mptooling/notifycat/main/mappings.example.yaml -o mappings.yaml
$EDITOR .env
$EDITOR mappings.yaml

# 2. validate mappings
docker run --rm --user $(id -u):$(id -g) -v "$PWD:/app" --env-file .env \
  ghcr.io/mptooling/notifycat:latest notifycat-mapping validate

# 3. preflight
docker run --rm --user $(id -u):$(id -g) -v "$PWD:/app" --env-file .env \
  ghcr.io/mptooling/notifycat:latest notifycat-doctor

# 4. run (detached, restart on reboot, host-loopback only — Caddy will reach it via 127.0.0.1:8080)
docker run -d --name notifycat --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  --user $(id -u):$(id -g) -v "$PWD:/app" --env-file .env \
  ghcr.io/mptooling/notifycat:latest

# 5. TLS + reverse proxy (clones this repo to get the script — or scp it over yourself)
git clone https://github.com/mptooling/notifycat.git /tmp/notifycat
sudo NOTIFYCAT_DOMAIN=notifycat.example.com \
     CADDY_EMAIL=ops@example.com \
     /tmp/notifycat/scripts/caddy-install.sh
```

After step 5, hit `https://notifycat.example.com/healthz` from your
browser — you should see `200 OK` with a valid Let's Encrypt cert.

### What `caddy-install.sh` does

The script is a single POSIX-sh file under `scripts/`. It is
**idempotent** — safe to re-run when you want to upgrade Caddy or
rewrite the Caddyfile from new env values.

1. Downloads the latest Caddy release binary from GitHub (override
   with `CADDY_VERSION`) for `amd64` / `arm64` / `armv7` and installs
   it to `/usr/local/bin/caddy`.
2. Creates the `caddy` system user, `/etc/caddy/`, `/var/lib/caddy/`.
3. Writes `/etc/caddy/Caddyfile`:

   ```caddyfile
   {
       email <CADDY_EMAIL>
   }

   <NOTIFYCAT_DOMAIN> {
       reverse_proxy 127.0.0.1:8080
       encode gzip zstd
   }
   ```

   (Any existing Caddyfile is backed up with a timestamped suffix.)
4. Installs the canonical Caddy systemd unit at
   `/etc/systemd/system/caddy.service` with hardening flags
   (`PrivateTmp`, `ProtectSystem=full`, `ProtectHome`, and
   `CAP_NET_BIND_SERVICE` so the unprivileged `caddy` user can bind
   80/443).
5. Runs `caddy fmt` + `caddy validate` before activating.
6. `systemctl enable --now caddy` on first run; `systemctl reload caddy`
   on subsequent runs.

**Auto-renewal is automatic.** Caddy refreshes its Let's Encrypt
certificates ~30 days before expiry, all in-process — no `cron`, no
`certbot.timer`, no extra config. Renewals are logged to
`journalctl -u caddy`.

### Verification

| Check | Command | Expected |
| --- | --- | --- |
| Caddy is running | `systemctl status caddy` | `active (running)` |
| Cert provisioned | `journalctl -u caddy --grep="certificate obtained successfully"` | one line per (sub)domain |
| Notifycat reachable through Caddy | `curl -i https://notifycat.example.com/healthz` | `HTTP/2 200` |
| Direct upstream reachable on the box | `curl -i http://127.0.0.1:8080/healthz` | `HTTP/1.1 200 OK` |
| Container restart survives reboot | `sudo reboot` then `docker ps` once SSH comes back | `notifycat` and `caddy` both running |

### Common HTTP-01 challenge failures

If `journalctl -u caddy` shows ACME errors:

| Symptom | Cause | Fix |
| --- | --- | --- |
| `connection refused` / `timeout` on the challenge | EC2 security group does not allow inbound 80 | Open port 80 to `0.0.0.0/0` |
| `no such host` / `NXDOMAIN` | DNS A record not yet propagated | Wait or check with `dig +short notifycat.example.com` |
| `unauthorized` | The DNS A record points at a different IP | `curl -s https://api.ipify.org` on the box vs `dig +short` from your laptop |
| `rate limited` | Repeated failures pushed you over LE's 5-per-week limit | Wait the cooldown out; or pre-test against the staging directory by setting `acme_ca https://acme-staging-v02.api.letsencrypt.org/directory` in the global Caddyfile block |

## Migrating from a `/data`-based deployment (pre-0.4.0)

`0.4.0` moves all state under `/app`. If your `0.3.x` `docker run`
mounted a volume at `/data` (and possibly `mappings.yaml` at
`/etc/notifycat/` or `/mappings.yaml`), migrate like this:

```sh
docker stop notifycat
docker rm   notifycat

# Move the SQLite DB into the new single state dir
mkdir -p ~/notifycat
mv /path/to/old-data-dir/notifycat.db ~/notifycat/notifycat.db

# Move mappings (if they were on a separate mount)
mv /path/to/old-mappings.yaml ~/notifycat/mappings.yaml
mv /path/to/old-mappings.lock ~/notifycat/mappings.lock   # optional

# Drop the `-v ...:/data`, `-v ...:/etc/notifycat/...`, and the
# `-e NOTIFYCAT_MAPPINGS_FILE=...` flags — they are now defaults.
# Re-run with the single mount:
docker run -d --name notifycat --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  --user $(id -u):$(id -g) -v "$HOME/notifycat:/app" --env-file ~/notifycat/.env \
  ghcr.io/mptooling/notifycat:latest
```

The migration does not touch the SQLite schema; existing
`slack_messages` rows continue to serve and update.

## Troubleshooting

**`mappings: write lock tmp: open mappings.lock.tmp: permission denied`**

You're on a pre-0.4.0 image where the bind-mounted file's parent
directory wasn't writable. Upgrade to `:0.4.0` (or `:latest`) and
follow the migration above. The new image's `WORKDIR=/app` puts the
lock file inside the mount, where atomic-write-and-rename works.

**`store: open: unable to open database file: out of memory (14)`**

The parent directory of `DATABASE_URL` does not exist or is not
writable by the container's user. With the image defaults, the DB is
at `/app/notifycat.db` — so the `/app` mount must be writable.
`--user $(id -u):$(id -g)` is the simplest fix; alternatively
`chown 65532:65532 ~/notifycat` once.

**Container exits immediately on start**

```sh
docker logs notifycat
```

The most common cause is `app: startup validation failed for N entries`
— one or more mappings failed Slack or GitHub checks. Run
`notifycat-mapping validate` separately to see the per-check detail.

**Caddy fails to obtain a certificate**

See the HTTP-01 failure table above. Worst case, switch to the
staging ACME endpoint while debugging:

```caddyfile
{
    email ops@example.com
    acme_ca https://acme-staging-v02.api.letsencrypt.org/directory
}
```

then `sudo systemctl reload caddy`. Staging certs are not trusted by
browsers but produce the same errors quickly without rate limits.

## Building the image locally

```sh
docker build -t notifycat:dev .
```

The justfile has `just docker-build`, `just docker-validate`,
`just docker-doctor`, `just docker-up` for the same flows against the
local build.
