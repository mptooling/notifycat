# notifycat

`notifycat` listens for GitHub pull request webhooks and keeps Slack up to
date. One pull request gets one Slack message. As the PR opens, moves to draft,
gets reviewed, merges, or closes, notifycat updates that message and adds the
configured reactions.

It is intentionally small: one HTTP endpoint, a SQLite database, and a CLI for
mapping GitHub repositories to Slack channels.

## What It Handles

- `pull_request` webhooks for opened, closed, and converted-to-draft PRs.
- `pull_request_review` webhooks for approved, commented, and
  changes-requested reviews.
- GitHub HMAC-SHA256 verification through `X-Hub-Signature-256`.
- Repository mappings in SQLite: `owner/repo -> Slack channel + mentions`.
- Slack message updates instead of repeated new messages for the same PR.

## Binaries

| Binary | Purpose |
| --- | --- |
| `notifycat-server` | HTTP server for GitHub webhooks |
| `notifycat-mapping` | CLI for repo-to-Slack mappings |
| `notifycat-migrate` | Applies embedded SQLite migrations |

## Quickstart

Create a local env file:

```sh
cp .env.example .env
```

Edit `.env` and set at least:

```sh
GITHUB_WEBHOOK_SECRET=your-webhook-secret
SLACK_BOT_TOKEN=xoxb-your-slack-bot-token
```

Apply migrations:

```sh
go run ./cmd/notifycat-migrate up
```

Add a repository mapping:

```sh
go run ./cmd/notifycat-mapping add owner/repo C123ABCDE @alice,@bob
```

Start the server:

```sh
go run ./cmd/notifycat-server
```

Check the health endpoint:

```sh
curl -i http://localhost:8080/healthz
```

## Mapping CLI

Mappings decide where a repository posts in Slack and who gets mentioned in the
message body.

```sh
notifycat-mapping add owner/repo C123ABCDE @alice,@bob
notifycat-mapping list
notifycat-mapping remove owner/repo
```

Repository names must use `owner/name` format. Slack channel IDs must look like
Slack IDs, for example `C123ABCDE`.

## GitHub Webhook

In the GitHub repository, create a webhook with:

| Field | Value |
| --- | --- |
| Payload URL | `https://your-domain.example/webhook/github` |
| Content type | `application/json` |
| Secret | Same value as `GITHUB_WEBHOOK_SECRET` |
| Events | `Pull requests` and `Pull request reviews` |

GitHub will sign each delivery. notifycat rejects requests with missing or
invalid signatures.

## Slack Setup

Create a Slack app with a bot token and invite the bot to every target channel.
The bot needs `chat:write`, `reactions:read`, and `reactions:write`.

Use the channel ID, not the channel name, in `notifycat-mapping add`.

## Docker

Build the image:

```sh
docker build -t notifycat .
```

Run migrations against a mounted data directory:

```sh
mkdir -p data
docker run --rm \
  -v "$PWD/data:/data" \
  -e GITHUB_WEBHOOK_SECRET=your-webhook-secret \
  -e SLACK_BOT_TOKEN=xoxb-your-slack-bot-token \
  notifycat /usr/local/bin/notifycat-migrate up
```

Run the server:

```sh
docker run --rm \
  -p 8080:8080 \
  -v "$PWD/data:/data" \
  -e GITHUB_WEBHOOK_SECRET=your-webhook-secret \
  -e SLACK_BOT_TOKEN=xoxb-your-slack-bot-token \
  notifycat
```

The image runs as UID/GID `65532:65532` and stores SQLite data at
`/data/notifycat.db` by default.

## Configuration

| Variable | Required | Default | Notes |
| --- | --- | --- | --- |
| `ADDR` | No | `:8080` | HTTP listen address |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn`, or `error` |
| `LOG_FORMAT` | No | `text` | `text` or `json` |
| `DATABASE_URL` | No | `file:./data/notifycat.db` | SQLite path or `file:` DSN |
| `GITHUB_WEBHOOK_SECRET` | Yes | - | GitHub webhook secret |
| `SLACK_BOT_TOKEN` | Yes | - | Slack bot token, usually `xoxb-...` |
| `SLACK_BASE_URL` | No | `https://slack.com` | Override for tests |
| `SLACK_REACTIONS_ENABLED` | No | `true` | Enables reaction updates |
| `SLACK_REACTION_NEW_PR` | No | `large_green_circle` | New PR reaction |
| `SLACK_REACTION_MERGED_PR` | No | `twisted_rightwards_arrows` | Merged PR reaction |
| `SLACK_REACTION_CLOSED_PR` | No | `x` | Closed PR reaction |
| `SLACK_REACTION_PR_APPROVED` | No | `white_check_mark` | Approved review reaction |
| `SLACK_REACTION_PR_COMMENTED` | No | `speech_balloon` | Commented review reaction |
| `SLACK_REACTION_PR_REQUEST_CHANGE` | No | `exclamation` | Changes-requested reaction |

## Security Notes

- Secrets are read from environment variables and should not be passed on the
  command line.
- Webhook signatures are compared with `hmac.Equal`.
- The webhook body is size-limited before JSON parsing.
- Server read, write, idle, and header timeouts are set explicitly.
- Request failures return generic HTTP errors; secrets and raw payloads are not
  echoed back.
- SQLite access goes through GORM parameter binding. Mapping CLI inputs are
  validated before they are stored.
- CI runs `go vet`, `golangci-lint`, `govulncheck`, `go test -race`, and
  `go build`.

## Development

Run the usual checks before pushing:

```sh
go vet ./...
golangci-lint run ./...
govulncheck ./...
go test -race ./...
go build ./...
```

## License

MIT. See [`LICENSE`](LICENSE).
