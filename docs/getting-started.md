# Getting Started

This guide gets Notifycat running on your machine with a local SQLite database. Use it when you want to test the full
GitHub-to-Slack flow before deploying. For the production path, see [Install with Docker Compose](compose.md) instead —
it covers the full install, HTTPS setup, and preflight checks.

## Requirements

- Go 1.25.10 or newer.
- `sh` and `curl` for the setup helper scripts.
- Permission to create a Slack app in your workspace. See [Slack app setup](slack-app.md).
- A GitHub repository where you can create webhooks. See [GitHub webhook setup](github-webhook.md).

## Clone the Repository

Clone Notifycat and work from the repository root. The setup scripts use files from this repository.

## Create the Slack App

Create the Slack app from the committed manifest:

```sh
SLACK_APP_CONFIG_TOKEN=xoxe-your-token ./scripts/slack-app-create.sh
```

Install the Slack app from its app settings page, then copy the **Bot User OAuth Token**. You will use it as
`SLACK_BOT_TOKEN`.

## Configure Local Environment

Create a local env file:

```sh
cp .env.example .env
```

Generate a GitHub webhook secret with your password manager, secret manager, or:

```sh
openssl rand -base64 32
```

Edit `.env` and set these values:

```sh
GITHUB_WEBHOOK_SECRET=your-32-plus-character-random-secret
SLACK_BOT_TOKEN=xoxb-your-slack-bot-token
```

The default database path is `file:./data/notifycat.db`. You can keep it for local work.

## Apply Migrations

Create the SQLite schema:

```sh
go run ./cmd/notifycat-migrate up
```

You can inspect migration state with:

```sh
go run ./cmd/notifycat-migrate status
```

## Add a Repository Mapping

Mappings tell Notifycat where each repository should post in Slack. They live in the `mappings:` section of `config.yaml` (default path `./config.yaml`, override with `NOTIFYCAT_CONFIG_FILE`). Start from the bundled example:

```sh
cp config.example.yaml config.yaml
```

Edit `config.yaml` so each org points at the right Slack channel:

```yaml
mappings:
  owner:
    channel: C123ABCDE             # the Slack channel ID, not "#name"
    mentions:
      - "<@U123456>"               # user
      - "<!subteam^S123456>"       # user group
    repositories:
      - repo
```

For a whole-org rule, use a wildcard:

```yaml
mappings:
  owner:
    channel: C123ABCDE
    mentions: ["<!channel>"]
    repositories: "*"
```

See [Mappings file](mappings.md) for the full schema, lookup rules, and the lock-file cache. Validate what you wrote against the live workspace before starting the server:

```sh
go run ./cmd/notifycat-config list      # show what's parsed
go run ./cmd/notifycat-config validate  # check each entry end-to-end
```

Repository names must use `owner/name` format. Slack channel IDs should be real Slack IDs such as `C123ABCDE`, not
display names like `#engineering`.

## Preflight with the Doctor

Before starting the server, run the doctor to surface any remaining configuration, database, or mappings issues:

```sh
go run ./cmd/notifycat-doctor                 # config + database + mappings file
go run ./cmd/notifycat-doctor owner/repo      # + Slack + GitHub webhook for one repo
```

Exit code is `0` on success and `1` on the first failure. See [Doctor](doctor.md) for the full check matrix.

On a first-time local setup the `github-webhook` check is **expected to fail or skip** here — the webhook is created
later in [Create the GitHub Webhook](#create-the-github-webhook). With `GITHUB_TOKEN` unset, the check reports `SKIP`;
with it set, the check reports `FAIL` because no webhook is registered yet. The same caveat applies to `notifycat-config validate` above. Both flip to `OK` once you re-run them after the webhook step.

## Start the Server

Run:

```sh
go run ./cmd/notifycat-server
```

Or, if you use `just` (`brew install just` on macOS):

```sh
just serve
```

`just` is a local development shortcut. It is not needed for production setup.

The server listens on `:8080` by default.

Check health:

```sh
curl -i http://localhost:8080/healthz
```

The webhook endpoint is:

```text
POST /webhook/github
```

## Create the GitHub Webhook

For local GitHub webhook testing, expose your running local server with a tunnel such as ngrok or Cloudflare Tunnel.

Then use the tunnel base URL as `NOTIFYCAT_PUBLIC_URL`:

```sh
GITHUB_TOKEN=github_pat_your-token \
GITHUB_WEBHOOK_SECRET=your-32-plus-character-random-secret \
NOTIFYCAT_PUBLIC_URL=https://your-tunnel.example \
./scripts/github-webhook-create.sh owner/repo
```

Use the same `GITHUB_WEBHOOK_SECRET` value in `.env` and in the GitHub webhook.

Re-run the doctor with `GITHUB_TOKEN` exported now that the webhook exists — the `github-webhook` check should flip to
`OK`:

```sh
GITHUB_TOKEN=github_pat_your-token \
  go run ./cmd/notifycat-doctor owner/repo
```

## Verify the Flow

After Slack and GitHub are configured:

1. Open a pull request in the mapped repository.
2. Confirm Notifycat posts one Slack message in the mapped channel.
3. Approve, comment, request changes, convert to draft, close, or merge the PR.
4. Add a line-specific comment on the diff.
5. Confirm the existing Slack message changes instead of a new message appearing for each event.

If GitHub reports a failed delivery, check the response code in the GitHub webhook delivery page and the Notifycat logs.
