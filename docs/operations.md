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
    Stale rows keyed by the old provider collide with the new one and silently suppress posts — details in [Upgrading](upgrading.md#git_provider-is-now-required).

## Logging

Set `server.log_format: json` in `config.yaml` for production log aggregation, and `server.log_level: debug` when triaging. Logs never contain raw webhook payloads, signatures, or secrets.

The one log contract worth knowing by heart: every intentionally-ignored delivery logs `ignored webhook event` with a `reason` field. That line is how you answer "the webhook returned 200 — why didn't Slack change?" from logs alone. The reason table lives in [Troubleshooting](troubleshooting.md#200-ok-no-slack-change).

## AI decisions

When the optional [AI layer](ai.md) is enabled, every advisor consultation emits an `ai decision` log line at `INFO` level. See [AI → The `ai decision` log line](ai.md#the-ai-decision-log-line) for the full field reference. The most operator-relevant field is `fallback_reason`: when non-empty, the model decision was not applied and the deterministic behavior ran instead.

| `fallback_reason` | Meaning | Operator action |
| --- | --- | --- |
| _(empty)_ | Model decision applied | none |
| `timeout` | Provider exceeded the 2.5 s decision deadline (10 s for digest) | check provider latency; consider a faster model |
| `transport_error` | Network/HTTP failure reaching the provider | check connectivity, `ai.base_url`, provider status |
| `rate_limited` | Provider returned 429/quota exhausted | check the provider quota console; `notifycat-doctor` shows headroom where exposed |
| `malformed_output` | Response was not valid schema JSON | usually transient; persistent → try another model |
| `guard_tripped` | PR content matched an injection heuristic | expected defense; inspect the PR if frequent |
| `clamp_violation` | Model chose out-of-bounds values; invalid fields were repaired | harmless; persistent → the model may be too weak for structured output |
| `circuit_open` | 5 consecutive provider failures; skipping calls for 10 min | provider outage; deliveries continue deterministically |
| `disabled` | The repo's tier opts out via `ai.enabled: false` | expected |

A non-empty `fallback_reason` is never an error in the operational sense — every fallback produces the same output as if AI were off. Use the reason to diagnose provider issues or to confirm that per-tier opt-outs are working.

## Release images

Each release publishes multi-arch images to `ghcr.io/mptooling/notifycat`; tags — including the per-PR beta images — are covered in [Supported tags](docker.md#supported-tags). To move a running Compose deployment to a new release: `docker compose pull && ./notifycat up`. Release-specific operator actions are collected in [Upgrading](upgrading.md).
