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

The release workflow publishes images to:

```text
ghcr.io/mptooling/notifycat:<tag>
ghcr.io/mptooling/notifycat:latest
```

Tag names should use the `v*` format, for example `v0.1.0`.

## Deployment Checklist

1. Create the Slack app and install the bot.
2. Create durable storage for `/data`.
3. Set `GITHUB_WEBHOOK_SECRET` and `SLACK_BOT_TOKEN`.
4. Run `notifycat-migrate up`.
5. Add repository mappings with `notifycat-mapping`.
6. Start `notifycat-server`.
7. Register the GitHub webhook.
8. Open a test pull request and confirm Slack receives one message.
9. Approve, comment, request changes, draft, close, or merge to confirm updates.

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
