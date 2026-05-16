# Operations

notifycat is designed to be operated as a single process plus a SQLite file.
There are no background workers and no external queue.

## Process Model

Run one `notifycat-server` process behind your normal HTTPS ingress. GitHub
posts webhooks to `/webhook/github`, and notifycat makes outbound HTTPS calls to
Slack.

The server exposes:

| Route | Purpose |
| --- | --- |
| `GET /healthz` | Liveness/readiness check. |
| `POST /webhook/github` | GitHub webhook receiver. |

## Startup and Shutdown

The server fails fast when required configuration is missing. It also applies
embedded migrations at startup, so a simple deployment can start the server
directly. For stricter production setups, run `notifycat-migrate up` as a
separate init step before the server starts.

Shutdown listens for `SIGINT` and `SIGTERM` and allows in-flight requests to
finish within the configured shutdown window.

## Persistence

SQLite stores:

- Repository-to-channel mappings.
- Pull request to Slack message timestamps.

Back up the SQLite file if losing notification state would be painful. If the
database is lost, notifycat can still receive webhooks, but existing PRs may get
new Slack messages because the old Slack timestamp mapping is gone.

## Logging

Use:

```sh
LOG_FORMAT=json
```

for production log aggregation. Logs avoid raw webhook payloads, signatures, and
secrets.

## Deploying a Release Image

The release workflow runs on `v*` tags and publishes images to:

```text
ghcr.io/mptooling/notifycat:<version>
ghcr.io/mptooling/notifycat:latest
```

For a Git tag such as `v0.1.0`, the version image tag is `0.1.0`.

## Deployment Checklist

1. Create the Slack app with `./scripts/slack-app-create.sh`, install the bot,
   and copy the bot token.
2. Create durable storage for `/data`.
3. Generate a long random `GITHUB_WEBHOOK_SECRET`.
4. Set `GITHUB_WEBHOOK_SECRET` and `SLACK_BOT_TOKEN`.
5. Run `notifycat-migrate up` if your deployment uses an explicit migration
   step. The server also applies pending migrations at startup.
6. Add repository mappings with `notifycat-mapping`.
7. Run `notifycat-mapping validate` to confirm the Slack side is wired
   correctly. With `GITHUB_TOKEN` exported, this also checks webhook event
   coverage.
8. Start `notifycat-server`.
9. Register the GitHub webhook with `./scripts/github-webhook-create.sh`.
10. Open a test pull request and confirm Slack receives one message.
11. Approve, comment, add a line-specific comment, request changes, draft,
    close, or merge to confirm updates.

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
| `mapping` | A row exists for `owner/repo` in SQLite. | `notifycat-mapping add owner/repo <channel-id> <mentions>` |
| `channel-format` | Stored channel ID matches `[CGD][A-Z0-9]{2,}`. | Re-run `add` with a valid channel ID copied from Slack. |
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
