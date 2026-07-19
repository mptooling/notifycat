# Configuration

This page is the complete key-by-key reference. New to Notifycat? Read [Configuration basics](configure.md) first — it shows the whole model (two files, one change loop) in two minutes; come back here to look up exact keys, defaults, and rules.

Notifycat is configured through a single `config.yaml` file that holds all non-secret settings. Secrets and infra-interpolation values live in environment variables (or `.env` for local development). For local development, `.env` is loaded automatically if the file exists.

The config file path defaults to `./config.yaml`. Override with `NOTIFYCAT_CONFIG_FILE`. The Docker image default is `/app/config.yaml`.

Secrets should stay in environment variables or your deployment secret manager. Do not put real tokens in committed files. See [config.example.yaml](https://github.com/mptooling/notifycat/blob/main/config.example.yaml) for a copy-paste starting point.

## Secrets (environment variables only)

Which webhook secret and read token apply depends on [`git_provider`](#git_provider): a github deployment uses the `GITHUB_*` variables, a bitbucket deployment uses the `BITBUCKET_*` variables. `SLACK_BOT_TOKEN` is always required; a deployment never needs the other provider's credentials.

| Variable | Required | Notes |
| --- | --- | --- |
| `SLACK_BOT_TOKEN` | Required | Slack bot token, usually starting with `xoxb-`. |
| `GITHUB_WEBHOOK_SECRET` | Required for `git_provider: github` | The secret configured on the GitHub webhook. Use a long random value; 32 characters or more is a good baseline. |
| `GITHUB_TOKEN` | Optional (github) | PAT used by `notifycat-config validate` and `notifycat-doctor` to read repository webhook config and by path routing to read a PR's changed files. Required scope: `admin:repo_hook` (or `repo` for private repositories). The server does not need this; if unset, the webhook-coverage and path-routing checks are skipped. |
| `BITBUCKET_WEBHOOK_SECRET` | Required for `git_provider: bitbucket` | The secret configured on the Bitbucket repository/workspace webhook. Bitbucket signs deliveries with `X-Hub-Signature: sha256=<hmac>`; an unsigned delivery (no secret configured) is rejected. |
| `BITBUCKET_TOKEN` | Optional (bitbucket) | Access token (Bearer) used by validate/doctor to read webhook config and by path routing to read changed files — exact `GITHUB_TOKEN` degradation parity. Least-privilege scopes: `repository` + `pullrequest` + `webhook`. |
| `BITBUCKET_AUTH_EMAIL` | Optional (bitbucket) | Pair with `BITBUCKET_TOKEN` to switch to HTTP Basic with a scoped Atlassian API token (the Free-plan path for workspace-wildcard listing). Unset ⇒ Bearer. |
| `AI_API_KEY` | Required for `ai.provider: gemini` | Model-provider API key for the optional [AI layer](ai.md). Optional for `openai_compatible` (keyless local endpoints). |

The server and CLIs fail fast when `SLACK_BOT_TOKEN` is missing, or when the webhook secret required by the selected [`git_provider`](#git_provider) is missing (`GITHUB_WEBHOOK_SECRET` for `git_provider: github`, `BITBUCKET_WEBHOOK_SECRET` for `git_provider: bitbucket`).

`GITHUB_TOKEN` is also read by `scripts/github-webhook-create.sh`, but that script *creates* the webhook and only needs the `Webhooks: Read and write` permission on a fine-grained PAT. The validate/doctor reading path needs `admin:repo_hook` / `repo`. A single token that has both works everywhere; otherwise issue separate PATs.

## Infra-interpolation (environment variables only)

These are not secrets but must live in `.env` because `docker-compose` and Caddy read them directly and cannot access `config.yaml`.

| Variable | Notes |
| --- | --- |
| `DOMAIN` | Public DNS name. Caddy uses it as the virtual-host name and obtains a Let's Encrypt certificate. Required when using `compose.yaml`. Also set `server.domain` in `config.yaml` so `notifycat-doctor` can derive the webhook URL. |
| `ACME_EMAIL` | Contact email for Let's Encrypt registration. Required when using `compose.yaml` — Caddy will fail to start without it. |

## config.yaml reference

### git_provider

| Key | Default | Notes |
| --- | --- | --- |
| `git_provider` | _(required)_ | The git host this deployment serves — `github` or `bitbucket`. An absent or unknown value fails startup with an error naming the key and pointing here. |

`git_provider` selects which webhook secret is required (`git_provider: github` ⇒ `GITHUB_WEBHOOK_SECRET`, `git_provider: bitbucket` ⇒ `BITBUCKET_WEBHOOK_SECRET`; `SLACK_BOT_TOKEN` is always required regardless), which `/webhook/...` route the server registers (`/webhook/github` or `/webhook/bitbucket`), and which provider's validation probes run. A deployment serves exactly one provider and never needs the other's credentials. Switching `git_provider` against an existing database requires a fresh database; see [Upgrading](upgrading.md#git_provider-is-now-required).

#### Bitbucket behavior notes

Two provider-specific behaviors are worth knowing when running `git_provider: bitbucket`:

- **Self-healing draft/ready transitions.** Bitbucket has no distinct "opened as draft" vs "marked ready" events — both arrive as `pullrequest:updated`. Notifycat reads the PR's `draft` flag on each update: `draft: true` converts the tracked message to draft (deletes it), and `draft: false` on an `OPEN` PR treats it as ready-for-review. An `updated` event on a non-`OPEN` PR is ignored. This means a PR toggled draft↔ready re-announces correctly without a dedicated event.
- **The user-account-bot blind spot.** Bot suppression keys on `actor.type != "user"` (so `team` and `app_user` actors are treated as bots). A bot that authenticates as an ordinary Bitbucket **user account** reports `actor.type: "user"` and is therefore indistinguishable from a human — its reviews are not suppressed even with `reviews.ignore_ai_reviews: true`. Prefer app-based integrations for reviewers you want suppressed.

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
| `github.base_url` | `https://api.github.com` | Override for GitHub Enterprise or tests. Used when `git_provider: github`. |

### bitbucket

| Key | Default | Notes |
| --- | --- | --- |
| `bitbucket.base_url` | `https://api.bitbucket.org/2.0` | Override for tests. Used when `git_provider: bitbucket`. |

### cleanup

| Key | Default | Notes |
| --- | --- | --- |
| `cleanup.message_ttl_days` | `30` | Days a `slack_messages` row may go without an update before the in-process cleanup removes it. Must be `> 0`. The cleanup runs once at startup and then once every 24 hours; it only deletes the DB row, never the actual Slack message. |

### reviews

| Key | Default | Notes |
| --- | --- | --- |
| `reviews.ignore_ai_reviews` | `false` | When `true`, suppress `reactions.add` for any review event whose `sender.type == "Bot"` — Copilot, Claude, Codex, dependabot, github-actions, release-please, and any other GitHub App or legacy bot account. Detection is intentionally coarse: notifycat does **not** distinguish AI reviewers from scripted bots. See [Reactions & bot reviews](bots-and-reactions.md#bot-reviewers) for the trade-off and failure-mode guide. |
| `reviews.dependabot_format` | `true` | When `true`, PRs opened by `dependabot[bot]` or `renovate[bot]` post a compact Slack message instead of the standard "please review" format: `:package: <bot> bumped <link>` for routine bumps, or `:rotating_light: <bot> security update <link>` when the PR body shows a security advisory. Set `false` to render those PRs with the standard format. See [Reactions & bot reviews → Dependabot and Renovate](bots-and-reactions.md#dependabot-and-renovate) for detection details. |

### digest

| Key | Default | Notes |
| --- | --- | --- |
| `digest.enabled` | `true` | Turns the stuck-PR digest on or off. The feature is on by default: omitting this section entirely keeps the digest running. |
| `digest.schedule` | `0 9 * * *` | Standard 5-field cron expression, evaluated in `digest.timezone`. An invalid expression fails server startup. |
| `digest.timezone` | `UTC` | IANA timezone name (e.g. `Europe/Kyiv`) the schedule and the "stuck since before today" cutoff are evaluated in. Absent/empty means UTC. An unrecognized zone fails server startup. Global only — a per-repository `digest:` override may not set it. |

### ai

The optional AI salience layer modulates notification loudness, routing emphasis, and digest ordering. It is off by default and its absence never affects delivery. See [AI](ai.md) for a full guide including provider setup and cost expectations.

| Key | Default | Notes |
| --- | --- | --- |
| `ai.enabled` | `false` | Turns the AI layer on or off. When `false` (the default), every decision is deterministic and no model calls are made. The server and all CLIs start normally regardless of this setting. |
| `ai.provider` | _(required when enabled)_ | Model provider: `gemini` or `openai_compatible`. An absent or unknown value fails startup when `ai.enabled: true`. |
| `ai.model` | _(required when enabled)_ | Model name passed to the provider (e.g. `gemini-2.0-flash`, `llama3.1`). Never defaulted — an absent value fails startup when `ai.enabled: true`. |
| `ai.base_url` | _(provider default)_ | Provider base URL. Optional for `gemini` (uses the public Gemini endpoint). Required for `openai_compatible` — an absent value fails startup when that provider is selected. |
| `ai.instructions` | _(empty)_ | Optional operator guidance text embedded in every prompt. Use it to steer the model toward your team's norms (e.g. "prioritize security-tagged PRs"). Concatenated with any per-tier instructions from `mappings:`. |

`AI_API_KEY` is the model-provider API key. It is env-only (never in `config.yaml`) and required when `ai.provider: gemini`. For `openai_compatible` it is optional — keyless local endpoints such as Ollama work without it.

### mappings

Routing from repositories to Slack channels lives in the `mappings:` section of `config.yaml`. The per-repository-tier schema (0.18+) organizes each org into named repository tiers plus an optional `"*"` catch-all tier. Each tier sets its own channel and optional mentions; repository tiers inherit from the `"*"` tier. Behavioral settings (`reactions`, `reviews`, `digest`) can also be overridden per-repository tier; when omitted they inherit from the `"*"` tier or fall back to the global values. See [Mappings](mappings.md) for the full schema reference and examples.

## Config CLI

The file is operated via the `notifycat-config` binary — `list` prints the parsed config, `validate` checks each entry end-to-end against Slack and the git host, with results cached in `config.lock` so only changed entries revalidate. The server runs the same validation at boot and refuses to start on failure. Commands, checks, and the remediation table: [CLI → notifycat-config](cli.md#notifycat-config).
