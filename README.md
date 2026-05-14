# notifycat

A tiny self-contained service that listens to GitHub pull-request webhooks and
posts/updates messages in Slack. Built to keep the notification thread for a
single PR in one place: the same Slack message gets edited as the PR moves
through review, with emoji reactions reflecting state.

## Status

Early development. Everything ships from `main`. Tagged releases will come once
the surface stabilises.

## What it does

- Receives `pull_request` and `pull_request_review` webhooks at
  `POST /webhook/github`.
- Verifies the GitHub HMAC-SHA256 signature (`X-Hub-Signature-256`).
- Maps a GitHub repository to a Slack channel + mentions.
- For each PR, posts one Slack message and edits it through the PR lifecycle.
- Adds emoji reactions for review state (approved, changes-requested,
  commented, merged, closed) and clears the message when a PR is converted
  back to draft.

## Components

`notifycat` ships as three small binaries:

| Binary               | Purpose                                                |
|----------------------|--------------------------------------------------------|
| `notifycat-server`   | HTTP server that handles GitHub webhooks               |
| `notifycat-mapping`  | CLI to manage repo → channel + mentions mappings       |
| `notifycat-migrate`  | Apply embedded database migrations                     |

## Quickstart

See [`docs/quickstart.md`](docs/quickstart.md) once it exists — until then,
the planned dev flow is:

```sh
cp .env.example .env
# fill GITHUB_WEBHOOK_SECRET and SLACK_BOT_TOKEN
go run ./cmd/notifycat-migrate up
go run ./cmd/notifycat-mapping add owner/repo C123ABCDE @alice,@bob
go run ./cmd/notifycat-server
```

## License

MIT. See [`LICENSE`](LICENSE).
