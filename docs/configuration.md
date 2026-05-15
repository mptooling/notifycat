# Configuration

notifycat reads configuration from environment variables. For local development,
it also loads `.env` if the file exists.

Secrets should stay in environment variables or your deployment secret manager.
Do not put real tokens in committed files.

## Required Variables

| Variable | Description |
| --- | --- |
| `GITHUB_WEBHOOK_SECRET` | The secret configured on the GitHub webhook. |
| `SLACK_BOT_TOKEN` | Slack bot token, usually starting with `xoxb-`. |

The server and CLIs fail fast when either value is missing.

## Server and Logging

| Variable | Default | Notes |
| --- | --- | --- |
| `ADDR` | `:8080` | HTTP listen address. |
| `LOG_LEVEL` | `info` | Supported values: `debug`, `info`, `warn`, `error`. |
| `LOG_FORMAT` | `text` | Use `json` for structured production logs. |

## Database

| Variable | Default | Notes |
| --- | --- | --- |
| `DATABASE_URL` | `file:./data/notifycat.db` | SQLite path or `file:` DSN. |

In Docker, the image sets:

```text
DATABASE_URL=file:/data/notifycat.db
```

Mount `/data` if you want the database to survive container restarts.

## Slack

| Variable | Default | Notes |
| --- | --- | --- |
| `SLACK_BASE_URL` | `https://slack.com` | Mostly useful for tests. |

## Reactions

| Variable | Default | Notes |
| --- | --- | --- |
| `SLACK_REACTIONS_ENABLED` | `true` | Turns reaction updates on or off. |
| `SLACK_REACTION_NEW_PR` | `large_green_circle` | Added when a PR is opened. |
| `SLACK_REACTION_MERGED_PR` | `twisted_rightwards_arrows` | Added when a PR is merged. |
| `SLACK_REACTION_CLOSED_PR` | `x` | Added when a PR is closed without merge. |
| `SLACK_REACTION_PR_APPROVED` | `white_check_mark` | Added when a review approves the PR. |
| `SLACK_REACTION_PR_COMMENTED` | `speech_balloon` | Added when a review comments on the PR. |
| `SLACK_REACTION_PR_REQUEST_CHANGE` | `exclamation` | Added when a review requests changes. |

Use Slack emoji names without surrounding colons. For example, set
`SLACK_REACTION_PR_APPROVED=shipit`, not `:shipit:`.

## Mapping Data

Routing is stored in SQLite, not in environment variables. Use
`notifycat-mapping`:

```sh
notifycat-mapping add owner/repo C123ABCDE '<@U123>,<!subteam^S123>'
notifycat-mapping list
notifycat-mapping remove owner/repo
```

Mentions are stored as plain Slack mention strings and joined into the message
body. User mentions look like `<@U123456>`. User group mentions look like
`<!subteam^S123456>`.
