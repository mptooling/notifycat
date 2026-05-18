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
Use a long random `GITHUB_WEBHOOK_SECRET`; 32 characters or more is a good
baseline.

## Server and Logging

| Variable | Default | Notes |
| --- | --- | --- |
| `ADDR` | `:8080` | HTTP listen address. |
| `LOG_LEVEL` | `info` | Supported values: `debug`, `info`, `warn`, `error`. |
| `LOG_FORMAT` | `text` | Use `json` for structured production logs. |

## Database

| Variable | Default | Notes |
| --- | --- | --- |
| `DATABASE_URL` | `file:./data/notifycat.db` | SQLite path or `file:` DSN. Stores the per-PR Slack message timestamps. Mappings live in YAML, not the database. |

In Docker, the image sets:

```text
DATABASE_URL=file:/data/notifycat.db
```

Mount `/data` if you want the database to survive container restarts.

## Mappings File

| Variable | Default | Notes |
| --- | --- | --- |
| `NOTIFYCAT_MAPPINGS_FILE` | `./mappings.yaml` | Path to the declarative mappings file. The sibling lock file (e.g. `./mappings.lock`) is derived by swapping the `.yaml`/`.yml` extension for `.lock`. |

Both `notifycat-server` and `notifycat-mapping` read this file. See
[Mappings file](mappings.md) for the schema and
[`mappings.example.yaml`](https://github.com/mptooling/notifycat/blob/main/mappings.example.yaml) for a copy-paste
starting point.

## Slack

| Variable | Default | Notes |
| --- | --- | --- |
| `SLACK_BASE_URL` | `https://slack.com` | Mostly useful for tests. |

## GitHub (validation only)

| Variable | Default | Notes |
| --- | --- | --- |
| `GITHUB_TOKEN` | _(unset)_ | Optional PAT used only by `notifycat-mapping validate` to read repo webhook config. Required scope: `admin:repo_hook` (or `repo` for private repos). The server does not need this; if unset, the webhook-coverage check is skipped. |
| `GITHUB_BASE_URL` | `https://api.github.com` | Override for GitHub Enterprise or tests. |

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

## Mapping CLI

Mappings are defined in [`mappings.yaml`](mappings.md), not in environment
variables. The `notifycat-mapping` binary has two subcommands:

```sh
notifycat-mapping list                    # print the parsed file (no network)
notifycat-mapping validate                # validate every entry, cache-aware
notifycat-mapping validate owner/repo     # validate a single entry, ignore cache for it
notifycat-mapping validate --force        # ignore the lock entirely; revalidate everything
```

`validate` checks each entry end-to-end: the Slack channel ID is
well-formed, the bot token has the required scopes, the bot is a member
of the channel, and (when `GITHUB_TOKEN` is set) the GitHub webhook is
subscribed to `pull_request`, `pull_request_review`, and
`pull_request_review_comment`. See `docs/operations.md` for the
failure-mode remediation table. The server runs the same validation at
boot and refuses to start on failure.
