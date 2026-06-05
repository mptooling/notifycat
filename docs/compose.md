# Install with Docker Compose (HTTPS)

The recommended production path: one `docker compose up -d` brings up Notifycat and a Caddy reverse proxy that obtains
and renews a Let's Encrypt certificate automatically. All state is kept in Docker named volumes — no host-directory
ownership concerns.

!!! tip "Recommended production path"
    Use this page for production installs. If you only want to run Notifycat locally or prefer to manage Caddy yourself,
    see [Docker (manual)](docker.md).

## Prerequisites

| Requirement | Notes |
| --- | --- |
| Docker Engine + Compose V2 | Run `docker compose version` — must be v2 (the `docker compose` subcommand, not `docker-compose`) |
| A domain name | An A (or AAAA) record pointing at the public IP of the host |
| Ports open | `80/tcp` (Let's Encrypt HTTP-01 challenge), `443/tcp` + `443/udp` (HTTPS + HTTP/3) must be reachable from the internet |
| `mappings.yaml` | Copied from `mappings.example.yaml` and edited (see step 3) |

## Quick-start

### 1. Run the installer

```sh
curl -fsSL https://raw.githubusercontent.com/mptooling/notifycat/main/scripts/install.sh | sh
cd notifycat
```

The installer checks that Docker and Compose V2 are present, creates a `./notifycat` directory, and downloads all
required files into it (`compose.yaml`, `Caddyfile`, `notifycat` wrapper, `.env.example`, `mappings.example.yaml`).

### 2. Run the setup wizard

```sh
./notifycat setup
```

The wizard prompts for:

- **Domain** — the public DNS name pointing at this host (e.g. `notifycat.acme.com`)
- **ACME email** — Let's Encrypt contact address
- **GitHub webhook secret** — any strong random string; you'll use it when registering the webhook
- **Slack bot token** — starts with `xoxb-`
- **First mapping** — GitHub org, repositories (`*` for all, or a comma-separated list), and Slack channel ID

It writes `.env` (permissions `0600`) and a starter `mappings.yaml`. Edit `mappings.yaml` to add more repos or orgs; see
[Mappings](mappings.md) for the full format reference.

### 3. Start the stack

```sh
docker compose up -d
```

Caddy contacts Let's Encrypt via the HTTP-01 challenge. First-time certificate provisioning typically completes within
30 seconds.

### 4. Verify

```sh
# HTTPS health check through Caddy
curl -i https://notifycat.example.com/healthz   # expect HTTP/2 200

# Preflight report (config, database, mappings)
./notifycat doctor
```

All doctor entries should show `ok`.

### 5. Smoke-test delivery before wiring the real webhook

The doctor confirms config and connectivity; the smoke test confirms the **whole path** actually delivers. It forges a
correctly-signed `pull_request: opened` event for a repository in your `mappings.yaml`, POSTs it to the running server's
`/webhook/github` (exercising the real signature check, dispatcher, and Slack client), and reports the channel and Slack
timestamp it produced:

```sh
./notifycat smoke <org>/<repo>              # use a repo present in mappings.yaml
./notifycat smoke --reactions <org>/<repo>  # also exercise the review-lifecycle reactions
```

A real message titled `[notifycat smoke] …` appears in the mapped channel — delete it once you've confirmed delivery. A
secret mismatch is reported as a clear `401`, and an unmapped repository is rejected before any request is sent.

Add `--reactions` to also replay a comment, an approval, and a merge for the same synthetic PR and verify (via
`reactions.get`) that the configured emoji landed on the message — an end-to-end check of `reactions:write`/`read` and the
reaction handlers. It is skipped with a note when `SLACK_REACTIONS_ENABLED=false`, and the merge step decorates the
message as `[Merged]`, so expect a few extra emoji on the throwaway message.

### 6. Register the GitHub webhook

Set your webhook URL to `https://notifycat.example.com/webhook/github` with the secret from `GITHUB_WEBHOOK_SECRET`. See
[GitHub webhook setup](github-webhook.md).

### 7. Run the security checklist before go-live

Walk the [Security & permissions](security.md) checklist — confirm `.env` is `0600`, the webhook secret is long and
random, and the Slack bot carries only its documented scopes. It also explains why the running server needs no GitHub
token at all.

## How the stack is wired

```
Internet ──HTTPS──▶ Caddy :443 ──HTTP──▶ notifycat :8080
```

Caddy terminates TLS and proxies to the `notifycat` service on the internal Docker network. Three named volumes hold all
persistent state:

| Volume | Contents |
| --- | --- |
| `notifycat_data` | SQLite database (`notifycat.db`) and `mappings.lock` |
| `caddy_data` | Let's Encrypt certificates and ACME state |
| `caddy_config` | Caddy runtime config |

`mappings.yaml` is bind-mounted read-only at `/app/mappings.yaml` inside the container. The writable `notifycat_data`
volume covers the rest of `/app`, so `mappings.lock` (which Notifycat writes as a sibling file) lives on the named
volume without needing write access to the bind mount.

## Managing the stack

```sh
./notifycat up                                  # start or recreate containers
./notifycat down                                # stop and remove containers (volumes preserved)
./notifycat logs                                # follow server logs
docker compose pull && ./notifycat up           # pull latest image and redeploy
docker compose logs -f caddy                    # follow Caddy logs (ACME, access)
./notifycat doctor                              # run preflight checks
```

Both containers are set to `restart: unless-stopped` — they start automatically on reboot.

## Troubleshooting

### Caddy fails to obtain a certificate

```sh
docker compose logs caddy
```

| Symptom | Cause | Fix |
| --- | --- | --- |
| `connection refused` / `timeout` on the ACME challenge | Port 80 blocked (firewall or security group) | Open inbound TCP 80 to `0.0.0.0/0` |
| `no such host` / `NXDOMAIN` | DNS A record not yet propagated | Wait, then check with `dig +short notifycat.example.com` |
| `unauthorized` | DNS points at a different IP than this host | Compare `curl -s https://api.ipify.org` on the host vs `dig +short` from your laptop |
| `rate limited` | Repeated failures exceeded Let's Encrypt's 5-failures-per-week limit | Wait out the cooldown; or test with the LE staging endpoint (see below) |

**Testing with the Let's Encrypt staging endpoint** (no rate limits, but cert is untrusted by browsers):

Edit `Caddyfile` and add `acme_ca` to the global block:

```caddyfile
{
    email ops@example.com
    acme_ca https://acme-staging-v02.api.letsencrypt.org/directory
}
```

Then restart Caddy: `docker compose restart caddy`. Remove the `acme_ca` line before go-live.

### Port 80 or 443 already in use

Caddy fails to bind if another process holds port 80 or 443.

```sh
sudo ss -tlnp | grep ':80\|:443'
```

Common causes: a running `nginx`/`apache2` service, a previous `docker run -p 443:443` container, or another Caddy
instance. Stop the conflicting process, then run `docker compose up -d caddy` again.

### UID 65532 permission errors on the named volume

Named volumes are initialised from the image's `/app` directory, which is already owned by `65532:65532` in the
published image — so this should not occur on fresh installs.

If you see `permission denied` after restoring a backup or pre-populating the volume:

```sh
# One-shot container to fix ownership (replace the volume name if yours differs)
docker run --rm \
  -v notifycat_notifycat_data:/app \
  alpine chown -R 65532:65532 /app
```

Run `docker volume ls` to confirm the exact volume name.

### Webhook returns 401

401 means the HMAC-SHA256 signature check failed — the secret on the GitHub webhook settings page does not match
`GITHUB_WEBHOOK_SECRET` in `.env`.

1. Copy the exact secret from GitHub → repository → Settings → Webhooks (no trailing whitespace).
2. Update `.env`, then restart: `docker compose restart notifycat`.
3. Redeliver the failing event from GitHub → Settings → Webhooks → Recent deliveries.

If the secret contains special shell characters, wrap it in single quotes in `.env`:

```
GITHUB_WEBHOOK_SECRET='p@$$w0rd!'
```

### Notifycat exits on startup

```sh
docker compose logs notifycat
```

The most common cause is `app: startup validation failed for N entries` — one or more mappings failed their Slack or
GitHub checks at boot. Run the mapping validator to see per-entry detail:

```sh
docker compose run --rm notifycat notifycat-mapping validate
```

Fix the failing entries in `mappings.yaml`, then `docker compose up -d` again. See [Operations](operations.md) for the
full ignored-event reason table.
