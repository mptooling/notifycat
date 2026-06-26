# Configuration

Notifycat is configured through a single `config.yaml` file that holds all non-secret settings. Secrets and infra-interpolation values live in environment variables (or `.env` for local development). For local development, `.env` is loaded automatically if the file exists.

The config file path defaults to `./config.yaml`. Override with `NOTIFYCAT_CONFIG_FILE`. The Docker image default is `/app/config.yaml`.

Secrets should stay in environment variables or your deployment secret manager. Do not put real tokens in committed files. See [config.example.yaml](https://github.com/mptooling/notifycat/blob/main/config.example.yaml) for a copy-paste starting point.

## Secrets (environment variables only)

| Variable | Required | Notes |
| --- | --- | --- |
| `GITHUB_WEBHOOK_SECRET` | Required | The secret configured on the GitHub webhook. Use a long random value; 32 characters or more is a good baseline. |
| `SLACK_BOT_TOKEN` | Required | Slack bot token, usually starting with `xoxb-`. |
| `GITHUB_TOKEN` | Optional | PAT used by `notifycat-config validate` and `notifycat-doctor` to read repo webhook config. Required scope: `admin:repo_hook` (or `repo` for private repos). The server does not need this; if unset, the webhook-coverage check is skipped. |

The server and CLIs fail fast when `GITHUB_WEBHOOK_SECRET` or `SLACK_BOT_TOKEN` is missing.

`GITHUB_TOKEN` is also read by `scripts/github-webhook-create.sh`, but that script *creates* the webhook and only needs the `Webhooks: Read and write` permission on a fine-grained PAT. The validate/doctor reading path needs `admin:repo_hook` / `repo`. A single token that has both works everywhere; otherwise issue separate PATs.

## Infra-interpolation (environment variables only)

These are not secrets but must live in `.env` because `docker-compose` and Caddy read them directly and cannot access `config.yaml`.

| Variable | Notes |
| --- | --- |
| `DOMAIN` | Public DNS name. Caddy uses it as the virtual-host name and obtains a Let's Encrypt certificate. Required when using `compose.yaml`. Also set `server.domain` in `config.yaml` so `notifycat-doctor` can derive the webhook URL. |
| `ACME_EMAIL` | Contact email for Let's Encrypt registration. Required when using `compose.yaml` — Caddy will fail to start without it. |

## config.yaml reference

### server

| Key | Default | Notes |
| --- | --- | --- |
| `server.addr` | `:8080` | HTTP listen address. |
| `server.log_level` | `info` | Supported values: `debug`, `info`, `warn`, `error`. |
| `server.log_format` | `text` | Use `json` for structured production logs. |
| `server.domain` | _(unset)_ | Public domain name. `notifycat-doctor` derives `https://$domain/webhook/github` from this. Not read by the server process itself. |

### database

| Key | Default | Notes |
| --- | --- | --- |
| `database.url` | `file:./data/notifycat.db` | SQLite path or `file:` DSN. Stores the per-PR Slack message timestamps. Mappings live in `config.yaml`, not the database. |

For Docker, set `database.url` in your mounted `config.yaml`. The recommended value is `file:/app/notifycat.db` (the historical image path) or `file:/data/notifycat.db` if you mount a `/data` volume for persistence.

### slack

| Key | Default | Notes |
| --- | --- | --- |
| `slack.base_url` | `https://slack.com` | Override for tests only. |
| `slack.reactions.enabled` | `true` | Turns reaction updates on or off. |
| `slack.reactions.new_pr` | `eyes` | Added when a PR is opened. Doubles as the leading emoji of the new-PR message. |
| `slack.reactions.merged_pr` | `twisted_rightwards_arrows` | Added when a PR is merged. |
| `slack.reactions.closed_pr` | `x` | Added when a PR is closed without merge. |
| `slack.reactions.approved` | `white_check_mark` | Added when a review approves the PR. |
| `slack.reactions.commented` | `speech_balloon` | Added when a review comments on the PR. |
| `slack.reactions.request_change` | `exclamation` | Added when a review requests changes. |
| `slack.reactions.bot_review` | `robot_face` | Distinct marker added **alongside** the normal reaction when a bot reviewer's activity is *not* suppressed (i.e. `reviews.ignore_ai_reviews: false`). Lets bot reviews stay visible but recognisable. Set empty to keep bot reviews indistinguishable from human ones. Mutually exclusive with suppression: when `reviews.ignore_ai_reviews: true` the bot reaction is skipped entirely, so this marker never appears. |

Use Slack emoji names without surrounding colons. For example, set `approved: shipit`, not `:shipit:`.

### github

| Key | Default | Notes |
| --- | --- | --- |
| `github.base_url` | `https://api.github.com` | Override for GitHub Enterprise or tests. |

### cleanup

| Key | Default | Notes |
| --- | --- | --- |
| `cleanup.message_ttl_days` | `30` | Days a `slack_messages` row may go without an update before the in-process cleanup removes it. Must be `> 0`. The cleanup runs once at startup and then once every 24 hours; it only deletes the DB row, never the actual Slack message. |

### reviews

| Key | Default | Notes |
| --- | --- | --- |
| `reviews.ignore_ai_reviews` | `false` | When `true`, suppress `reactions.add` for any review event whose `sender.type == "Bot"` — Copilot, Claude, Codex, dependabot, github-actions, release-please, and any other GitHub App or legacy bot account. Detection is intentionally coarse: notifycat does **not** distinguish AI reviewers from scripted bots. See [Operations → Bot-reviewer suppression](operations.md#bot-reviewer-suppression) for the trade-off and failure-mode guide. |
| `reviews.dependabot_format` | `true` | When `true`, PRs opened by `dependabot[bot]` or `renovate[bot]` post a compact Slack message instead of the standard "please review" format: `:package: <bot> bumped <link>` for routine bumps, or `:rotating_light: <bot> security update <link>` when the PR body shows a security advisory. Set `false` to render those PRs with the standard format. See [Operations → Dependabot / Renovate format](operations.md#dependabot--renovate-format) for detection details. |

### digest

| Key | Default | Notes |
| --- | --- | --- |
| `digest.enabled` | `true` | Turns the stuck-PR digest on or off. The feature is on by default: omitting this section entirely keeps the digest running. |
| `digest.schedule` | `0 9 * * *` | Standard 5-field cron expression, evaluated in `digest.timezone`. An invalid expression fails server startup. |
| `digest.timezone` | `UTC` | IANA timezone name (e.g. `Europe/Kyiv`) the schedule and the "stuck since before today" cutoff are evaluated in. Absent/empty means UTC. An unrecognized zone fails server startup. Global only — a per-repo `digest:` override may not set it. |

### mappings

Routing from repositories to Slack channels lives in the `mappings:` section of `config.yaml`. The per-repository-tier schema (0.18+) organizes each org into named repo tiers plus an optional `"*"` catch-all tier. Each tier sets its own channel and optional mentions; repo tiers inherit from the `"*"` tier. Behavioral settings (`reactions`, `reviews`, `digest`) can also be overridden per-repo tier; when omitted they inherit from the `"*"` tier or fall back to the global values. See [Mappings](mappings.md) for the full schema reference and examples.

## Config CLI

Mappings and the full configuration are operated via the `notifycat-config` binary:

```sh
notifycat-config list                    # print the parsed config (no network)
notifycat-config validate                # validate every entry, cache-aware
notifycat-config validate owner/repo     # validate a single entry, ignore cache for it
notifycat-config validate --force        # ignore the lock entirely; revalidate everything
```

`validate` checks each entry end-to-end: the Slack channel ID is well-formed, the bot token has the required scopes, the bot is a member of the channel, and (when `GITHUB_TOKEN` is set) the GitHub webhook is subscribed to `pull_request`, `pull_request_review`, and `pull_request_review_comment`. See `docs/operations.md` for the failure-mode remediation table. The server runs the same validation at boot and refuses to start on failure.

Successful validation results are cached in `config.lock`. On the next boot, only entries whose hash has changed are revalidated.
