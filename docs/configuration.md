# Configuration

Notifycat reads configuration from environment variables. For local development, it also loads `.env` if the file
exists.

Secrets should stay in environment variables or your deployment secret manager. Do not put real tokens in committed
files.

## Required Variables

| Variable | Description |
| --- | --- |
| `GITHUB_WEBHOOK_SECRET` | The secret configured on the GitHub webhook. |
| `SLACK_BOT_TOKEN` | Slack bot token, usually starting with `xoxb-`. |

The server and CLIs fail fast when either value is missing. Use a long random `GITHUB_WEBHOOK_SECRET`; 32 characters or
more is a good baseline.

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

Both `notifycat-server` and `notifycat-mapping` read this file. See [Mappings file](mappings.md) for the schema and
[`mappings.example.yaml`](https://github.com/mptooling/notifycat/blob/main/mappings.example.yaml) for a copy-paste
starting point.

## Slack

| Variable | Default | Notes |
| --- | --- | --- |
| `SLACK_BASE_URL` | `https://slack.com` | Mostly useful for tests. |

## GitHub (validation only)

| Variable | Default | Notes |
| --- | --- | --- |
| `GITHUB_TOKEN` | _(unset)_ | Optional PAT used by `notifycat-mapping validate` and `notifycat-doctor` to read repo webhook config. Required scope: `admin:repo_hook` (or `repo` for private repos). The server does not need this; if unset, the webhook-coverage check is skipped. |
| `GITHUB_BASE_URL` | `https://api.github.com` | Override for GitHub Enterprise or tests. |

`GITHUB_TOKEN` is also read by `scripts/github-webhook-create.sh`, but that script *creates* the webhook and only needs
the `Webhooks: Read and write` permission on a fine-grained PAT. The validate/doctor reading path needs
`admin:repo_hook` / `repo`. A single token that has both works everywhere; otherwise issue separate PATs.

## Cleanup

| Variable | Default | Notes |
| --- | --- | --- |
| `NOTIFYCAT_MESSAGE_TTL_DAYS` | `30` | Days a `slack_messages` row may go without an update before the in-process cleanup removes it. Must be `> 0`. The cleanup runs once at startup and then once every 24 hours; it only deletes the DB row, never the actual Slack message. |

## Reviewer suppression

| Variable | Default | Notes |
| --- | --- | --- |
| `NOTIFYCAT_IGNORE_AI_REVIEWS` | `false` | When `true`, suppress `reactions.add` for any review event whose `sender.type == "Bot"` — Copilot, Claude, Codex, dependabot, github-actions, release-please, and any other GitHub App or legacy bot account. Detection is intentionally coarse: notifycat does **not** distinguish AI reviewers from scripted bots. See [Operations → Bot-reviewer suppression](operations.md#bot-reviewer-suppression) for the trade-off and failure-mode guide. |

## Dependabot / Renovate format

| Variable | Default | Notes |
| --- | --- | --- |
| `NOTIFYCAT_DEPENDABOT_FORMAT` | `true` | When `true`, PRs opened by `dependabot[bot]` or `renovate[bot]` post a compact Slack message instead of the standard "please review" format: `:package: <bot> bumped <link>` for routine bumps, or `:rotating_light: <bot> security update <link>` when the PR body shows a security advisory. Set `false` to render those PRs with today's standard format. See [Operations → Dependabot / Renovate format](operations.md#dependabot--renovate-format) for detection details. |

## Reactions

| Variable | Default | Notes |
| --- | --- | --- |
| `SLACK_REACTIONS_ENABLED` | `true` | Turns reaction updates on or off. |
| `SLACK_REACTION_NEW_PR` | `eyes` | Added when a PR is opened. Doubles as the leading emoji of the new-PR message. |
| `SLACK_REACTION_MERGED_PR` | `twisted_rightwards_arrows` | Added when a PR is merged. |
| `SLACK_REACTION_CLOSED_PR` | `x` | Added when a PR is closed without merge. |
| `SLACK_REACTION_PR_APPROVED` | `white_check_mark` | Added when a review approves the PR. |
| `SLACK_REACTION_PR_COMMENTED` | `speech_balloon` | Added when a review comments on the PR. |
| `SLACK_REACTION_PR_REQUEST_CHANGE` | `exclamation` | Added when a review requests changes. |
| `SLACK_REACTION_BOT_REVIEW` | `robot_face` | Distinct marker added **alongside** the normal reaction when a bot reviewer's activity is *not* suppressed (i.e. `NOTIFYCAT_IGNORE_AI_REVIEWS=false`). Lets bot reviews stay visible but recognisable. Set empty (`SLACK_REACTION_BOT_REVIEW=`) to keep bot reviews indistinguishable from human ones. Mutually exclusive with suppression: when `NOTIFYCAT_IGNORE_AI_REVIEWS=true` the bot reaction is skipped entirely, so this marker never appears. |

Use Slack emoji names without surrounding colons. For example, set `SLACK_REACTION_PR_APPROVED=shipit`, not `:shipit:`.

## Mapping CLI

Mappings are defined in [`mappings.yaml`](mappings.md), not in environment variables. The `notifycat-mapping` binary has
two subcommands:

```sh
notifycat-mapping list                    # print the parsed file (no network)
notifycat-mapping validate                # validate every entry, cache-aware
notifycat-mapping validate owner/repo     # validate a single entry, ignore cache for it
notifycat-mapping validate --force        # ignore the lock entirely; revalidate everything
```

`validate` checks each entry end-to-end: the Slack channel ID is well-formed, the bot token has the required scopes, the
bot is a member of the channel, and (when `GITHUB_TOKEN` is set) the GitHub webhook is subscribed to `pull_request`,
`pull_request_review`, and `pull_request_review_comment`. See `docs/operations.md` for the failure-mode remediation
table. The server runs the same validation at boot and refuses to start on failure.
