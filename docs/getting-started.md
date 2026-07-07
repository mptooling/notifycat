# Run from source

Build and run Notifycat from a clone, with a local SQLite database — the path for contributors and for testing the full webhook-to-Slack flow before deploying. For production, use [Install with Docker Compose](compose.md) instead.

## Requirements

- Go 1.25.10 or newer.
- `sh` and `curl` for the setup helper scripts.
- Permission to create a Slack app in your workspace — see [Slack app setup](slack-app.md).
- A repository where you can create webhooks — see [GitHub webhook setup](github-webhook.md).

## Clone and configure

Clone the repository and work from its root — the setup scripts live there.

Create the Slack app from the committed manifest, install it from its settings page, and copy the **Bot User OAuth Token**:

```sh
SLACK_APP_CONFIG_TOKEN=xoxe-your-token ./scripts/slack-app-create.sh
```

Then create your local env file and fill in the secrets ([why the secret is generated exactly this way](security.md#generating-the-webhook-secret)):

```sh
cp .env.example .env
openssl rand -base64 32          # generate a webhook secret
```

Edit `.env`:

```sh
GITHUB_WEBHOOK_SECRET=your-generated-secret
SLACK_BOT_TOKEN=xoxb-your-slack-bot-token
```

The default database path is `file:./data/notifycat.db` — fine for local work.

## Apply migrations

```sh
go run ./cmd/notifycat-migrate up
go run ./cmd/notifycat-migrate status   # inspect state
```

## Add a mapping

Routing lives in the `mappings:` section of `config.yaml` (default path `./config.yaml`, overridable with `NOTIFYCAT_CONFIG_FILE`). Start from the bundled example:

```sh
cp config.example.yaml config.yaml
```

```yaml
mappings:
  owner:
    repo:
      channel: C123ABCDE             # the Slack channel ID, not "#name"
      mentions:
        - "<@U123456>"
```

The full schema, inheritance, and the `"*"` catch-all are covered in [Route repositories to channels](routing.md). Validate what you wrote against the live workspace:

```sh
go run ./cmd/notifycat-config list      # show what's parsed
go run ./cmd/notifycat-config validate  # check each entry end-to-end
```

## Preflight with the doctor

```sh
go run ./cmd/notifycat-doctor                 # config + database + mappings file
go run ./cmd/notifycat-doctor owner/repo      # + Slack + webhook checks for one repository
```

On a first-time setup the `webhook` check is **expected to fail or skip** — the webhook doesn't exist yet. With `GITHUB_TOKEN` unset it reports `SKIP`; with it set, `FAIL`. Both flip to `OK` after the webhook step below. The same caveat applies to `notifycat-config validate`.

## Start the server

```sh
go run ./cmd/notifycat-server    # or: just serve
```

The server listens on `:8080`. Check health and note the webhook endpoint:

```sh
curl -i http://localhost:8080/healthz
# webhook receiver: POST /webhook/github
```

## Create the webhook

GitHub needs a public URL, so expose the local server with a tunnel (ngrok, Cloudflare Tunnel), then create the webhook with the tunnel URL:

```sh
GITHUB_TOKEN=github_pat_your-token \
GITHUB_WEBHOOK_SECRET=your-generated-secret \
NOTIFYCAT_PUBLIC_URL=https://your-tunnel.example \
./scripts/github-webhook-create.sh owner/repo
```

Use the same `GITHUB_WEBHOOK_SECRET` value in `.env` and in the webhook. Re-run the doctor with `GITHUB_TOKEN` exported — the `webhook` check should now report `OK`.

## Verify the flow

1. Open a pull request in the mapped repository.
2. Confirm Notifycat posts one Slack message in the mapped channel.
3. Approve, comment, request changes, convert to draft, close, or merge the PR.
4. Confirm the existing message changes — reactions accumulate, no new messages appear.

If a delivery fails, start from the response code in the webhook delivery page and [Troubleshooting](troubleshooting.md).
