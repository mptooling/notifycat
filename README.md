# Notifycat

<p align="center">
  <img src="docs/assets/logo.png" alt="Notifycat logo" width="160">
</p>

[![CI](https://github.com/mptooling/notifycat/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/mptooling/notifycat/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/mptooling/notifycat?display_name=tag&sort=semver)](https://github.com/mptooling/notifycat/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/mptooling/notifycat)](go.mod) [![Go Report
Card](https://goreportcard.com/badge/github.com/mptooling/notifycat)](https://goreportcard.com/report/github.com/mptooling/notifycat)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE) [![Conventional
Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-yellow.svg)](https://www.conventionalcommits.org)

**Low-noise pull request notifications for Slack.** One pull request gets one Slack message: as the PR opens, gets reviewed, merges, or closes, that message updates in place and collects reactions instead of posting again. The channel becomes a status board, not an event log.

<img src="docs/assets/images/slack_notifications.png" width="900" alt="A Slack channel where every pull request is one updating message">

Three things it's built around:

- **Quiet** — state changes are message updates and emoji, not new posts; dependency bumps collapse to one compact line; bot reviews can be marked or muted.
- **Nothing slips through** — a morning digest resurfaces PRs nobody touched yesterday, and the "Start review" button shows who's already on it.
- **Easy to own** — one Go binary, one declarative `config.yaml`, one SQLite file. The server validates its whole configuration against Slack and your git host before it will boot, and runtime needs only a webhook secret plus a Slack bot token — no GitHub App, no OAuth.

## Documentation

Please visit <https://mptooling.github.io/notifycat/>

## Quick start

### Docker (recommended)

**You'll need** a host with Docker installed, a domain name pointing at it, inbound ports 80/443 open, and a Slack app installed in your workspace ([setup](https://mptooling.github.io/notifycat/slack-app/)). **In about 10 minutes** you'll have Notifycat running behind automatic HTTPS and posting PR updates to Slack.

```sh
curl -fsSL https://github.com/mptooling/notifycat/releases/latest/download/install.sh | sh
cd notifycat
./notifycat setup          # interactive wizard — writes .env and config.yaml
docker compose up -d       # start Notifycat + Caddy (HTTPS via Let's Encrypt)
./notifycat doctor         # verify setup
```

The installer downloads a pinned, checksum-verified bundle into `./notifycat`. The setup wizard prompts for your domain, Slack token, webhook secret, and first mapping. For the full walkthrough — webhook registration, a delivery smoke test, and verifying with a real PR — see [Install with Docker Compose](https://mptooling.github.io/notifycat/compose/), then run through the [Security & permissions](https://mptooling.github.io/notifycat/security/) checklist before go-live.

### Alternative: run from source (contributors)

Most users want the one-command path above. Build from source if you're contributing or want to run without Docker.

**Requires:**

- Go 1.25.10 or newer (`go version` to check).
- `git` to clone the repository.
- `sh` and `curl` for the helper scripts under `scripts/`.
- A public URL (ngrok or Cloudflare Tunnel) only if you want your git host to deliver real webhooks to your laptop. Local CLI commands (validate / doctor) don't need one.

Six commands from "nothing" to "running":

```sh
git clone https://github.com/mptooling/notifycat.git && cd notifycat
cp .env.example .env                       # then edit: set GITHUB_WEBHOOK_SECRET, SLACK_BOT_TOKEN
cp config.example.yaml config.yaml        # then edit: database.url and real Slack channel IDs

go run ./cmd/notifycat-config validate
go run ./cmd/notifycat-doctor
go run ./cmd/notifycat-server
```

The binaries pick up `.env` from the current working directory and default to `./config.yaml` and `./data/notifycat.db`. See [Run from source](https://mptooling.github.io/notifycat/getting-started/) for the end-to-end walkthrough including the tunnel + webhook setup.

## What you see in Slack

One message per PR: opening it posts the message, reviews land on it as reactions (✅ 💬 ❗), and merging strikes the title through. A **Start review** button shows who's already reviewing. Dependabot and Renovate bumps collapse to one compact line, and a morning digest resurfaces the PRs nobody touched yesterday. The full tour, message by message: [What you see in Slack](https://mptooling.github.io/notifycat/features/).

## Git Provider Support

| Feature | GitHub | Bitbucket |
| --- | --- | --- |
| Webhook signature verification (HMAC-SHA256) | Yes | Yes |
| Per-path / monorepo routing | Yes (requires `GITHUB_TOKEN`) | Yes (requires `BITBUCKET_TOKEN`) |
| Stuck-PR digest | Yes | Yes |
| Reactions & review flow | Yes | Yes |
| Token auth | Fine-grained PAT (Bearer) | Access token (Bearer) or scoped Atlassian API token (Basic, Free-plan fallback) |
| App passwords | n/a | **Not supported** — removed by Atlassian 2026-07-28; use access tokens |

A deployment serves one git host, and routing lives in the `mappings:` section of `config.yaml` — per-repository tiers with an org-wide `"*"` catch-all. See [Configuration basics](https://mptooling.github.io/notifycat/configure/).

## Development

The project includes a `justfile` for common development commands. Install [`just`](https://github.com/casey/just) (`brew install just` on macOS), then run:

```sh
just
just check
just serve
```

`just` is a developer tool only. It is not part of the Go module, the Docker runtime image, or production dependencies.

The underlying checks are:

```sh
go vet ./...
golangci-lint run ./...
govulncheck ./...
go test -race ./...
go build ./...
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for contributor setup, pull request expectations, and issue reporting guidance.

## Community

- [Code of conduct](CODE_OF_CONDUCT.md)
- [Support](SUPPORT.md)
- [Security policy](SECURITY.md)

## License

MIT. See [`LICENSE`](LICENSE).
