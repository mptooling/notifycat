# notifycat

`notifycat` listens for GitHub pull request webhooks and keeps Slack up to
date.

One pull request gets one Slack message. As the PR opens, moves to draft, gets
reviewed, merges, or closes, notifycat updates that message and adds the
configured reactions. The result is a quieter channel: reviewers can follow the
state of a PR without digging through repeated notifications.

It is intentionally small: one HTTP endpoint, a SQLite database, and a CLI for
mapping GitHub repositories to Slack channels.

## What It Handles

- `pull_request` webhooks for opened, closed, and converted-to-draft PRs.
- `pull_request_review` webhooks for approved, commented, and
  changes-requested reviews.
- `pull_request_review_comment` webhooks for line-specific PR comments.
- GitHub HMAC-SHA256 verification through `X-Hub-Signature-256`.
- Repository mappings in SQLite: `owner/repo -> Slack channel + mentions`.
- Slack message updates instead of repeated new messages for the same PR.

## Binaries

| Binary | Purpose |
| --- | --- |
| `notifycat-server` | HTTP server for GitHub webhooks |
| `notifycat-mapping` | CLI for repo-to-Slack mappings |
| `notifycat-migrate` | Applies embedded SQLite migrations |

## Documentation

- [Getting started](docs/getting-started.md)
- [Configuration](docs/configuration.md)
- [Slack app setup](docs/slack-app.md)
- [GitHub webhook setup](docs/github-webhook.md)
- [Docker](docs/docker.md)
- [Operations](docs/operations.md)

## Quickstart

Create a local env file and set the two required secrets:

```sh
cp .env.example .env
```

Then migrate, add your first repository mapping, and start the server:

```sh
go run ./cmd/notifycat-migrate up
go run ./cmd/notifycat-mapping add owner/repo C123ABCDE @alice,@bob
go run ./cmd/notifycat-server
```

Health check:

```sh
curl -i http://localhost:8080/healthz
```

See [Getting started](docs/getting-started.md) for the full local setup.

## Development

The project includes a `justfile` for common development commands. Install
[`just`](https://github.com/casey/just) (`brew install just` on macOS), then run:

```sh
just
just check
just serve
```

`just` is a developer tool only. It is not part of the Go module, the Docker
runtime image, or production dependencies.

The underlying checks are:

```sh
go vet ./...
golangci-lint run ./...
govulncheck ./...
go test -race ./...
go build ./...
```

## License

MIT. See [`LICENSE`](LICENSE).
