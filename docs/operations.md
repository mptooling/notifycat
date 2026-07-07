# Operations

Notifycat is designed to be operated as a single process plus a SQLite file. No background workers, no external queue, no runtime dependencies beyond Slack and your git host.

## Process model

Run one `notifycat-server` behind your normal HTTPS ingress. The git host posts webhooks in; Notifycat makes outbound HTTPS calls to Slack. The server exposes:

| Route | Purpose |
| --- | --- |
| `GET /healthz` | Liveness/readiness. Returns `200` once the listener is up — and because startup validation and migrations run *before* the listener opens, a `200` means the process is healthy in both senses. Wire it as the target-group health check or Kubernetes probe path. |
| `POST /webhook/github` | GitHub receiver — registered only when `git_provider: github`. |
| `POST /webhook/bitbucket` | Bitbucket receiver — registered only when `git_provider: bitbucket`. |

Only the configured provider's route exists; posting a Bitbucket payload to `/webhook/github` (or vice versa) returns `404`.

## Startup and shutdown

The server fails fast on missing or invalid configuration — see [Server exits at startup](troubleshooting.md#server-exits-at-startup) for the causes it names. It applies embedded migrations at startup, so a simple deployment starts the server directly; stricter setups can run `notifycat-migrate up` as a separate init step first.

Shutdown listens for `SIGINT`/`SIGTERM` and lets in-flight requests finish within the shutdown window.

## Persistence and backup

State lives in two places:

- **`config.yaml`** — the declarative source of truth for routing and all non-secret settings. Keep it in version control and deploy it alongside the binary. The sibling `config.lock` caches successful validation so steady-state boots don't re-contact Slack or the git host.
- **SQLite** — per-PR Slack message timestamps, so Notifycat can keep updating the same message across the PR lifecycle.

Back up the SQLite file if losing notification state would hurt. If it's lost, webhooks still work — but PRs that were already announced get fresh Slack messages, because the old timestamp mapping is gone. `config.yaml` and `config.lock` live in your ops repository, so a lost container copy is harmless.

A background cleanup removes `slack_messages` rows untouched for longer than `cleanup.message_ttl_days` (default 30), once at startup and then every 24 hours. It deletes only the database row — never the Slack message.

!!! warning "Changing `git_provider` requires a fresh database"
    The provider is not recorded per row. Pointing an existing database at a different `git_provider` lets stale rows — keyed by the old provider's repository names and PR numbering — collide with the new provider's, silently suppressing posts until the cleanup TTL purges them. Start from a fresh database when you switch. See [Upgrading](upgrading.md#git_provider-is-now-required).

## Logging

Set `server.log_format: json` in `config.yaml` for production log aggregation, and `server.log_level: debug` when triaging. Logs never contain raw webhook payloads, signatures, or secrets.

The one log contract worth knowing by heart: every intentionally-ignored delivery logs `ignored webhook event` with a `reason` field. That line is how you answer "the webhook returned 200 — why didn't Slack change?" from logs alone. The reason table lives in [Troubleshooting](troubleshooting.md#200-ok-no-slack-change).

## Release images

The release workflow runs on `v*` tags and publishes multi-arch images to `ghcr.io/mptooling/notifycat` — tag semantics (pinned / minor / major / `latest`) are in [Supported tags](docker.md#supported-tags). To move a running Compose deployment to a new release: `docker compose pull && ./notifycat up`. Release-specific operator actions are collected in [Upgrading](upgrading.md).

Every open same-repository PR also publishes a `pr-<number>` beta image for pre-merge testing — see [Supported tags](docker.md#supported-tags).
