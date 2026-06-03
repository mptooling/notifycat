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
[Install with Docker Compose](compose.md). That page covers the
one-command installer, the interactive setup wizard, HTTPS via
Let's Encrypt, and the full preflight checklist.

For local first-time setup against a tunnel (Go source, no Docker), see
[Getting started](getting-started.md). For the manual Docker + host Caddy
alternative, see
[Docker → Production deploy on a single VM](docker.md#production-deploy-on-a-single-vm-ec2-example).

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

## Bot-reviewer suppression

When `NOTIFYCAT_IGNORE_AI_REVIEWS=true`, notifycat skips the
`reactions.add` call for any review event whose `sender.type == "Bot"`. The
initial PR-open message and all human-reviewer events are unaffected.

### What it covers

The toggle silences every GitHub App and legacy bot account on review and
review-comment events. That includes, but is not limited to:

| Category | Examples |
| --- | --- |
| AI reviewers | Copilot review, Claude review, Codex, Goose, CodeRabbit, Reviewpad. |
| Scripted bots | `dependabot[bot]`, `renovate[bot]`, `github-actions[bot]` auto-approve workflows, `release-please[bot]` self-approvals. |
| Custom CI bots | Anything whose `sender.type` GitHub reports as `Bot`. |

### Bots ≠ AI agents — the explicit trade-off

GitHub's payload does not distinguish AI reviewers from scripted bots. The
only signal available is `sender.type == "Bot"`. We deliberately do **not**
maintain a curated AI-agent allowlist — vendor GitHub Apps get renamed
(Copilot's review bot has already been renamed twice) and such a list rots
faster than the operator value it provides.

If you enable `NOTIFYCAT_IGNORE_AI_REVIEWS`, you are opting into a
**uniform** rule: every non-human reviewer is silenced. If a team wants
their `github-actions[bot]` auto-approve green checkmark to surface in
Slack, they should leave the flag off.

This is also intentionally narrow:

- It only affects `reactions.add`. The initial `chat.postMessage` (with
  mentions / `@channel` fallback) is unchanged.
- It does not look at the PR **author** — a bot-authored PR (e.g.
  dependabot-created) still posts a new Slack message when it opens.
- It does not touch `mappings.yaml`. No schema change, no migration, no
  per-mapping override.

### Failure-mode guide

> "I expected a green checkmark / speech bubble / red flag on my PR's
> Slack message, but nothing happened."

1. Check `notifycat-server` logs at debug level. A line like
   `level=DEBUG msg="skipped bot reviewer reaction" login=copilot[bot] …`
   confirms the suppression fired.
2. If you see that log, `NOTIFYCAT_IGNORE_AI_REVIEWS` is on and the
   reviewer's GitHub account is a Bot/App. Either:
   - Disable the flag (`NOTIFYCAT_IGNORE_AI_REVIEWS=false`) — every bot
     reviewer will react again, AI or not.
   - Or accept the trade-off and leave the flag on.
3. If you do **not** see that log, the suppression did not fire; the
   missing reaction has a different cause — work through the regular
   "silent 200 OK" debug checklist above.

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
