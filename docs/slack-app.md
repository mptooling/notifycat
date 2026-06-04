# Slack App Setup

Notifycat posts to Slack with a bot token. You need one Slack app in the workspace where PR notifications should appear.

For production setup, use the shell script directly. It only needs `sh` and `curl`; `jq` is optional and only makes the
output easier to read.

## Create the App from the Manifest

The repository includes the Slack app manifest at `docs/slack-app-manifest.json`. The manifest defines the bot user and
the Slack scopes Notifycat needs.

Create a Slack app configuration token from [Slack API: Your Apps](https://api.slack.com/apps). The token lives under
the workspace-level **Your App Configuration Tokens** panel on that page — scroll past the apps list, generate a new
token, and copy the `xoxe-…` value. Then run:

```sh
SLACK_APP_CONFIG_TOKEN=xoxe-your-token ./scripts/slack-app-create.sh
```

For Enterprise Grid or org-level configuration tokens, pass the workspace ID:

```sh
SLACK_APP_CONFIG_TOKEN=xoxe-your-token \
SLACK_TEAM_ID=T123 \
./scripts/slack-app-create.sh
```

The script validates the required inputs before calling Slack. It does not store the configuration token, and it does
not belong in Notifycat production configuration.

After the app is created:

1. Open the app settings page printed by the script, or open [Slack API: Your Apps](https://api.slack.com/apps) and
   select the new app.
2. Go to **Install App**.
3. Install the app to the workspace.
4. Copy the **Bot User OAuth Token**.
5. Set that token as `SLACK_BOT_TOKEN` in Notifycat.

```sh
SLACK_BOT_TOKEN=xoxb-your-token
```

The Slack API response can include an `oauth_authorize_url`. Do not use that URL for Notifycat setup. That URL starts a
full OAuth callback flow, and Notifycat does not implement the redirect handler or code exchange. Use **Install App** in
the Slack app settings instead.

## Local Development Shortcut

If you use `just` while working on the repository, this recipe calls the same script:

```sh
SLACK_APP_CONFIG_TOKEN=xoxe-your-token just slack-app-create
```

Production instructions should use `./scripts/slack-app-create.sh` directly so operators do not need to install `just`.

## Manual Fallback

If the API-based setup is not available in your workspace, create the app in the Slack UI:

1. Open [Slack API: Your Apps](https://api.slack.com/apps).
2. Select **Create New App**.
3. Choose **From scratch**.
4. Pick the workspace and name the app `notifycat`.
5. Open **OAuth & Permissions**.
6. Add the bot scopes listed below.
7. Click **Install to Workspace**.
8. Copy the **Bot User OAuth Token** and set it as `SLACK_BOT_TOKEN`.

## Bot Scopes

| Scope | Why Notifycat needs it |
| --- | --- |
| `chat:write` | Post, update, and delete PR messages. |
| `chat:write.public` | Post into public channels without inviting the bot first. |
| `reactions:write` | Add configured PR-state reactions. |
| `channels:read` | Used by `notifycat-mapping validate` to confirm the bot can see the target channel. Add `groups:read` as well if you map private channels. |

The manifest includes these scopes. If you create the app manually, add the same scopes in **OAuth & Permissions**.

`notifycat-mapping validate` reads `X-OAuth-Scopes` from Slack's `auth.test` response and fails fast when `chat:write`
or `reactions:write` is missing.

## Channel Access

Invite the bot to every channel listed in `mappings.yaml`:

```text
/invite @notifycat
```

This is required for reaction updates. `chat:write.public` lets Notifycat post the first message in public channels
without joining them, but Slack may reject `reactions.add` unless the bot is a channel member. Inviting the bot keeps
both message posting and reactions working for public and private channels.

Use the channel ID in mappings, not the display name. In Slack, open channel details and copy the channel ID. It usually
starts with `C`.

## Mentions

Each `mappings.yaml` entry lists the Slack mentions that prefix the PR notification:

```yaml
mappings:
  owner:
    channel: C123ABCDE
    mentions:
      - "<@U123456>"
      - "<!subteam^S123456>"
    repositories: ["repo"]
```

Common formats:

| Mention type | Format |
| --- | --- |
| User | `<@U123456>` |
| User group | `<!subteam^S123456>` |
| Channel broadcast | `<!channel>` |
| Online-only broadcast | `<!here>` |

For a user ID, open the user profile menu in Slack and use **Copy member ID**. For a user group ID, inspect the user
group mention in Slack or use Slack's admin/API tooling.
