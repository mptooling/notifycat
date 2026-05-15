# Slack App Setup

notifycat talks to Slack through a bot token. Create one Slack app for the
workspace where notifications should appear.

## Create the App

1. Open [Slack API: Your Apps](https://api.slack.com/apps).
2. Select **Create New App**.
3. Choose **From scratch**.
4. Pick a workspace and name the app.

## Add Bot Scopes

In **OAuth & Permissions**, add these bot token scopes:

| Scope | Why notifycat needs it |
| --- | --- |
| `chat:write` | Post and update PR messages. |
| `reactions:read` | Read existing reactions before updating state. |
| `reactions:write` | Add configured PR-state reactions. |

If you want the bot to post into public channels without being invited first,
Slack may also require `chat:write.public`. The simpler setup is to invite the
bot to each channel that will receive notifications.

## Install the App

1. Click **Install to Workspace**.
2. Approve the requested scopes.
3. Copy the **Bot User OAuth Token**.
4. Set it as `SLACK_BOT_TOKEN`.

```sh
SLACK_BOT_TOKEN=xoxb-your-token
```

## Invite the Bot to Channels

Invite the bot to every channel used by `notifycat-mapping`:

```text
/invite @notifycat
```

Use the channel ID in mappings, not the display name. In Slack, open channel
details and copy the channel ID. It usually starts with `C`.

## Mentions

The mapping CLI accepts a comma-separated mention string:

```sh
notifycat-mapping add owner/repo C123ABCDE '<@U123456>,<!subteam^S123456>'
```

Common formats:

| Mention type | Format |
| --- | --- |
| User | `<@U123456>` |
| User group | `<!subteam^S123456>` |

For a user ID, open the user profile menu in Slack and use **Copy member ID**.
For a user group ID, inspect the user group mention in Slack or use Slack's
admin/API tooling.
