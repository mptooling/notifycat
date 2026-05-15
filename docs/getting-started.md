# Getting Started

This guide gets notifycat running on your machine with a local SQLite database.
Use it when you want to test the full GitHub-to-Slack flow before deploying.

## Requirements

- Go 1.25.10 or newer.
- A Slack app with a bot token. See [Slack app setup](slack-app.md).
- A GitHub repository where you can create webhooks. See
  [GitHub webhook setup](github-webhook.md).

## Clone and Configure

Create a local env file:

```sh
cp .env.example .env
```

Edit `.env` and set these values:

```sh
GITHUB_WEBHOOK_SECRET=your-webhook-secret
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
go run ./cmd/notifycat-mapping add owner/repo C123ABCDE @alice,@bob
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

The server listens on `:8080` by default.

Check health:

```sh
curl -i http://localhost:8080/healthz
```

The webhook endpoint is:

```text
POST /webhook/github
```

For local GitHub webhook testing, expose your local server with a tunnel such as
ngrok or Cloudflare Tunnel, then register the public URL in GitHub.

## Verify the Flow

After Slack and GitHub are configured:

1. Open a pull request in the mapped repository.
2. Confirm notifycat posts one Slack message in the mapped channel.
3. Approve, comment, request changes, convert to draft, close, or merge the PR.
4. Confirm the existing Slack message changes instead of a new message appearing
   for each event.

If GitHub reports a failed delivery, check the response code in the GitHub
webhook delivery page and the notifycat logs.
