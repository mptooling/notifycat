# Getting Started

This guide gets notifycat running on your machine with a local SQLite database.
Use it when you want to test the full GitHub-to-Slack flow before deploying.

## Requirements

- Go 1.25.10 or newer.
- `sh` and `curl` for the setup helper scripts.
- Permission to create a Slack app in your workspace. See
  [Slack app setup](slack-app.md).
- A GitHub repository where you can create webhooks. See
  [GitHub webhook setup](github-webhook.md).

## Clone the Repository

Clone notifycat and work from the repository root. The setup scripts use files
from this repository.

## Create the Slack App

Create the Slack app from the committed manifest:

```sh
SLACK_APP_CONFIG_TOKEN=xoxe-your-token ./scripts/slack-app-create.sh
```

Install the Slack app from its app settings page, then copy the **Bot User OAuth
Token**. You will use it as `SLACK_BOT_TOKEN`.

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

The default database path is `file:./data/notifycat.db`. You can keep it for
local work.

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

Mappings tell notifycat where each repository should post in Slack.

```sh
go run ./cmd/notifycat-mapping add owner/repo C123ABCDE '<@U123456>,<!subteam^S123456>'
```

List mappings:

```sh
go run ./cmd/notifycat-mapping list
```

Remove a mapping:

```sh
go run ./cmd/notifycat-mapping remove owner/repo
```

Repository names must use `owner/name` format. Slack channel IDs should be real
Slack IDs such as `C123ABCDE`, not display names like `#engineering`.

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

For local GitHub webhook testing, expose your running local server with a tunnel
such as ngrok or Cloudflare Tunnel.

Then use the tunnel base URL as `NOTIFYCAT_PUBLIC_URL`:

```sh
GITHUB_TOKEN=github_pat_your-token \
GITHUB_WEBHOOK_SECRET=your-32-plus-character-random-secret \
NOTIFYCAT_PUBLIC_URL=https://your-tunnel.example \
./scripts/github-webhook-create.sh owner/repo
```

Use the same `GITHUB_WEBHOOK_SECRET` value in `.env` and in the GitHub webhook.

## Verify the Flow

After Slack and GitHub are configured:

1. Open a pull request in the mapped repository.
2. Confirm notifycat posts one Slack message in the mapped channel.
3. Approve, comment, request changes, convert to draft, close, or merge the PR.
4. Confirm the existing Slack message changes instead of a new message appearing
   for each event.

If GitHub reports a failed delivery, check the response code in the GitHub
webhook delivery page and the notifycat logs.
