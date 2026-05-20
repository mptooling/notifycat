# Operations

Notifycat is designed to be operated as a single process plus a SQLite file.
There are no background workers and no external queue.

## Process Model

Run one `notifycat-server` process behind your normal HTTPS ingress. GitHub
posts webhooks to `/webhook/github`, and Notifycat makes outbound HTTPS calls to
Slack.

The server exposes:

| Route | Purpose |
| --- | --- |
| `GET /healthz` | Liveness/readiness check. Returns `200 OK` once the HTTP listener is up — wire it as the target-group health check (ALB, nginx upstream, Cloud Run) or the `livenessProbe`/`readinessProbe` path in Kubernetes. Because startup validation and migrations run before the listener opens, a `200` means the process is healthy in both senses. |
| `POST /webhook/github` | GitHub webhook receiver. |

## Startup and Shutdown

The server fails fast when required configuration is missing. It also applies
embedded migrations at startup, so a simple deployment can start the server
directly. For stricter production setups, run `notifycat-migrate up` as a
separate init step before the server starts.

Shutdown listens for `SIGINT` and `SIGTERM` and allows in-flight requests to
finish within the configured shutdown window.

## Persistence

State lives in two places:

- **`mappings.yaml`** — the declarative source of truth for routing. Edit
  it in version control and deploy it alongside the binary. The sibling
  `mappings.lock` caches successful validation so steady-state boots
  don't re-contact Slack/GitHub.
- **SQLite** — stores per-PR Slack message timestamps so Notifycat can
  update the same message across the PR lifecycle.

Back up the SQLite file if losing notification state would be painful. If
the database is lost, Notifycat can still receive webhooks, but existing
PRs may get new Slack messages because the old Slack timestamp mapping is
gone. `mappings.yaml` and `mappings.lock` live in your repo, so losing
the container's local copy is harmless on the next deploy.

## Logging

Use:

```sh
LOG_FORMAT=json
```

for production log aggregation. Logs avoid raw webhook payloads, signatures, and
secrets.

### Debugging a 200 OK with no Slack change

GitHub records a successful delivery whenever Notifycat returns 200 — including
when the event is intentionally ignored. Every silent no-op leaves a structured
log line with the message `ignored webhook event` and a `reason` field, so an
operator can answer *"why didn't Slack change for delivery X?"* from logs alone.

Standard field set (all six fields appear on every `ignored webhook event` line):

| Field | Example |
| --- | --- |
| `reason` | `no_handler`, `no_mapping`, `no_stored_message`, `already_sent` |
| `handler` | `open`, `close`, `draft`, `approve`, `commented`, `request_change` (empty for the dispatcher) |
| `github_event` | `pull_request`, `pull_request_review`, `pull_request_review_comment` |
| `action` | `opened`, `closed`, `synchronize`, `labeled`, `submitted`, … |
| `repository` | `owner/repo` |
| `pr` | PR number (int) |

Reasons and their levels:

| `reason` | Level | What it means | Typical fix |
| --- | --- | --- | --- |
| `no_handler` | **Debug** | No registered handler matched this `(github_event, action)` pair. Volumetric — fires for `synchronize`, `labeled`, `edited`, etc. | Expected; set `LOG_LEVEL=debug` to see it. |
| `no_mapping` | **Warn** | Webhook arrived for a repo no `mappings.yaml` entry covers. | Add the repo to `mappings.yaml` (or remove the webhook from that repo). |
| `no_stored_message` | **Info** | Handler ran but found no Slack message row for this PR. Common when the PR predates Notifycat. | Re-open the PR (or wait for the next applicable event) so `OpenHandler` can re-announce. |
| `already_sent` | **Info** | `OpenHandler` saw an existing message row — idempotency kicks in. | Expected on `ready_for_review` after a prior `opened`. |

To surface `no_handler` lines during triage:

```sh
LOG_LEVEL=debug
```

`grep` for the `reason` value (or filter on it in your log aggregator) to slice
silent deliveries by class.

## Deploying a Release Image

The release workflow runs on `v*` tags and publishes images to:

```text
ghcr.io/mptooling/notifycat:<version>
ghcr.io/mptooling/notifycat:latest
```

For a Git tag such as `v0.1.0`, the version image tag is `0.1.0`.

## Deployment

For end-to-end deploy instructions, see
[Docker → Production deploy on a single VM](docker.md#production-deploy-on-a-single-vm-ec2-example).
That page covers the five-command Docker run, the Caddy install for
HTTPS + Let's Encrypt auto-renewal, the security-group / DNS
prerequisites, and the migration path from pre-0.4.0 `/data`
deployments. For local first-time setup against a tunnel, see
[Getting started](getting-started.md).

The remainder of this page is operations-time reference: what each
log line means, what the validate / doctor checks cover, and how to
trace a silent 200-OK delivery.

<!-- Stale anchor preserved for old links to operations.md#deployment-checklist -->
<a id="deployment-checklist"></a>

## Validating a Mapping

`notifycat-mapping validate` is a non-destructive command that surfaces setup
problems before GitHub fires a real PR event. It exits 0 when every check
passes (or is skipped) and 1 when any check fails.

```sh
notifycat-mapping validate                 # check every mapping
notifycat-mapping validate owner/repo      # check a single mapping
```

Each line in the output is `STATUS  check-name  detail`. `OK`/`FAIL`/`SKIP`
are plain ASCII so the output is greppable in CI logs.

| Check | What it verifies | How to fix a `FAIL` |
| --- | --- | --- |
| `mapping` | An entry exists in `mappings.yaml` for `owner/repo` (explicit or wildcard). | Add the repo to that org's `repositories` list, or set `repositories: "*"`. See [Mappings file](mappings.md). |
| `channel-format` | The entry's channel ID matches `[CGD][A-Z0-9]{2,}`. | Edit `mappings.yaml` and use the real Slack channel ID, not the display name. |
| `slack-auth` | `auth.test` succeeds and `X-OAuth-Scopes` includes `chat:write` and `reactions:write`. | Rotate `SLACK_BOT_TOKEN`, or reinstall the app after updating the manifest scopes. |
| `slack-channel` | `conversations.info` reports the channel exists, is not archived, and the bot is a member. | `/invite @notifycat` in the channel; unarchive if needed; correct the channel ID. |
| `github-webhook` | When `GITHUB_TOKEN` is set, an active webhook on the repo points at `/webhook/github` and subscribes to `pull_request`, `pull_request_review`, `pull_request_review_comment`. Skipped when `GITHUB_TOKEN` is unset. | Create the webhook with `./scripts/github-webhook-create.sh`, or edit the existing webhook to add the missing events. |

### Tokens and Scopes for `validate`

- `SLACK_BOT_TOKEN`: the same bot token the server uses. Scopes
  `chat:write`, `reactions:write`, and `channels:read` (or `groups:read` for
  private channels) cover every probe.
- `GITHUB_TOKEN` (optional): a PAT with `admin:repo_hook` (or `repo` if the
  repository is private). Only the validate CLI consumes this; the server
  ignores it.

## CI Checks

The repository CI runs:

```sh
go vet ./...
golangci-lint run ./...
govulncheck ./...
go test -race ./...
go build ./...
```

The Docker build uses a patched Go toolchain. Keep the Go patch version current
when Go security releases land.

For local development, the same checks are available through `just check`.
`just` is only a task runner; it is not included in production builds or Go
module dependencies.
