# Install with Docker Compose (HTTPS)

The recommended production path: one `docker compose up -d` brings up Notifycat and a Caddy reverse proxy that obtains and renews a Let's Encrypt certificate automatically. All state lives in Docker named volumes — no host-directory ownership concerns.

!!! tip "Recommended production path"
    Use this page for production installs. To run locally, or if you manage Caddy yourself, see [Manual Docker](docker.md).

## Prerequisites

| Requirement | Notes |
| --- | --- |
| Docker Engine + Compose V2 | `docker compose version` must report v2 (the `docker compose` subcommand, not `docker-compose`) |
| A domain name | An A (or AAAA) record pointing at the public IP of the host |
| Ports open | `80/tcp` (Let's Encrypt HTTP-01 challenge), `443/tcp` + `443/udp` (HTTPS + HTTP/3) reachable from the internet |
| A Slack app | Created and installed in your workspace — see [Slack app setup](slack-app.md). Do this first: the setup wizard asks for its bot token |
| Git host access | Permission to create a webhook on the repository (or organization). The registration itself happens in step 6, after the server is live |

The order is deliberate: **Slack app → this stack → webhook**. The wizard needs the Slack bot token up front, and the webhook is registered last because the git host pings the endpoint on creation — it should be live and answering first.

## Quick start

### 1. Run the installer

```sh
curl -fsSL https://github.com/mptooling/notifycat/releases/latest/download/install.sh | sh
cd notifycat
```

The installer does:

- checks that Docker and Compose V2 are present
- creates a `./notifycat` directory
- downloads the stack files into the freshly created directory: `compose.yaml`, `Caddyfile`, the `notifycat` wrapper, `.env.example`, `config.example.yaml`
- verifies each against the release's `SHA256SUMS` before use

### 2. Run the setup wizard

```sh
./notifycat setup
```

The wizard prompts for your **domain**, an **ACME email** for Let's Encrypt, a **webhook secret**, your **Slack bot token**, and a **first config** (org, repositories, Slack channel ID). It writes `.env` with `0600` permissions and a starter `config.yaml`.

To add more repositories or orgs afterwards, edit `config.yaml` — [Configuration basics](configure.md) shows the model, [Route repositories to channels](routing.md) the recipes.

### 3. Start the stack

```sh
docker compose up -d
```

Caddy contacts Let's Encrypt via the HTTP-01 challenge; first-time certificate provisioning typically completes within 30 seconds.

### 4. Verify

```sh
curl -i https://notifycat.example.com/healthz   # expect HTTP/2 200, through Caddy
./notifycat doctor                              # preflight: config, database, mappings
```

All doctor entries should show `ok`.

### 5. Smoke-test delivery

The doctor proves the configuration; the smoke test proves the **whole path** — it forges a correctly-signed PR event, POSTs it to the running server, and reports the Slack channel and timestamp it produced:

```sh
./notifycat smoke <org>/<repo>              # a repository present in config.yaml
```

A real message titled `[notifycat smoke] …` appears in the mapped channel — delete it once confirmed. Add `--reactions` to also replay a comment, approval, and merge and verify the emoji landed. Details: [CLI → notifycat-smoke](cli.md#notifycat-smoke).

### 6. Register the webhook

Point your git host at `https://notifycat.example.com/webhook/github` (or `/webhook/bitbucket`) with the secret from step 2. Follow [GitHub webhook setup](github-webhook.md) or [Bitbucket webhook setup](bitbucket-webhook.md).

### 7. Verify with a real pull request

Open a pull request in a mapped repository and watch the product work:

1. One message appears in the mapped channel.
2. Approve or comment on the PR — a reaction lands on that same message; no new post.
3. Merge it — the title strikes through and the merged reaction appears.

If the message never shows up, start at [Troubleshooting → No message for a new PR](troubleshooting.md#no-message-for-a-new-pr).

### 8. Run the security checklist

Before go-live, walk the [Security & permissions](security.md) checklist — `.env` permissions, secret strength, bot scopes. It also explains why the running server needs no git-host token at all.

## How the stack is wired

```
Internet ──HTTPS──▶ Caddy :443 ──HTTP──▶ notifycat :8080
```

Caddy handles TLS and proxies to the `notifycat` service on the internal Docker network. Three named volumes hold all persistent state:

| Volume | Contents |
| --- | --- |
| `notifycat_data` | SQLite database (`notifycat.db`) and `config.lock` |
| `caddy_data` | Let's Encrypt certificates and ACME state |
| `caddy_config` | Caddy runtime config |

`config.yaml` is bind-mounted read-only at `/app/config.yaml`; the writable `notifycat_data` volume covers the rest of `/app`, so `config.lock` (written as a sibling) lands on the volume.

!!! warning "Keep `database.url` under `/app`"
    The volume mounts at `/app`, so only paths under `/app` persist. The default `file:./data/notifycat.db` resolves inside the volume; a path like `file:/data/notifycat.db` lands in the container's ephemeral layer and is **lost on every recreate** — including `docker compose pull && up`. If the database seems to vanish after an upgrade, it's almost always sitting in the volume at a different path: see [Troubleshooting → Database issues](troubleshooting.md#database-issues).

## Managing the stack

```sh
./notifycat up                                  # start or recreate containers
./notifycat down                                # stop and remove containers (volumes preserved)
./notifycat logs                                # follow server logs
./notifycat doctor                              # preflight checks
docker compose logs -f                          # follows logs with tail
```

Both containers use `restart: unless-stopped`, so they come back on reboot.

### Upgrading and pinning a version

The shipped `compose.yaml` tracks `ghcr.io/mptooling/notifycat:latest`, but `docker compose up` reuses whatever image is local — to actually move to a newer release, pull first:

```sh
docker compose pull && ./notifycat up
```

For reproducible deploys, pin the `image:` line to a specific `vX.Y.Z` instead of tracking `latest`. Tag semantics: [Supported tags](docker.md#supported-tags). Release-specific upgrade steps: [Upgrading](upgrading.md).

## When something fails

Every failure mode of this stack has a runbook in [Troubleshooting](troubleshooting.md):

- Caddy can't get a certificate, or ports 80/443 are taken → [Certificate failures](troubleshooting.md#certificate-failures)
- Webhook deliveries return 401 → [Webhook returns 401](troubleshooting.md#webhook-returns-401)
- The `notifycat` container exits immediately → [Server exits at startup](troubleshooting.md#server-exits-at-startup)
- Volume permissions or a "disappearing" database → [Database issues](troubleshooting.md#database-issues)
