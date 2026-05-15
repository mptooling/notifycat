# Slack App Setup

notifycat talks to Slack through a bot token. Create one Slack app for the
workspace where notifications should appear.

## Recommended: Create from Manifest

The repository includes a Slack app manifest at
`docs/slack-app-manifest.json`. Use it as the source of truth for the app's bot
user, OAuth scopes, and basic settings.

To create the app from the manifest, generate a Slack app configuration token
from [Slack API: Your Apps](https://api.slack.com/apps), then run:

```sh
SLACK_APP_CONFIG_TOKEN=xoxe-your-token just slack-app-create
```

For Enterprise Grid or org-level configuration tokens, pass the workspace ID:

```sh
SLACK_APP_CONFIG_TOKEN=xoxe-your-token SLACK_TEAM_ID=T123 just slack-app-create
```

After the app is created, open the app settings page, go to **Install App**,
install it to the workspace, then copy the **Bot User OAuth Token**.

The API response may include an `oauth_authorize_url`. Do not use that URL for
this setup unless notifycat grows a full OAuth callback flow. Slack's OAuth URL
requires a configured redirect URL and an app-side code exchange, while
notifycat currently expects a bot token that an operator copies from the app
settings page.

App configuration tokens are setup-only credentials. Do not store them in
production configuration. The helper script only requires `sh` and `curl`; if
`jq` is installed, it also prints the app ID and app settings URL in a shorter
form.

## Fallback: Create Manually

1. Open [Slack API: Your Apps](https://api.slack.com/apps).
2. Select **Create New App**.
3. Choose **From scratch**.
4. Pick a workspace and name the app.

## Add Bot Scopes

In **OAuth & Permissions**, add these bot token scopes:

| Scope | Why notifycat needs it |
| --- | --- |
| `chat:write` | Post, update, and delete PR messages. |
| `chat:write.public` | Post into public channels without inviting the bot first. |
| `reactions:write` | Add configured PR-state reactions. |

The manifest includes all scopes above. If you create the app manually, add the
same scopes in **OAuth & Permissions**.

## Install the App

1. Click **Install to Workspace**.
2. Approve the requested scopes.
3. Copy the **Bot User OAuth Token**.
4. Set it as `SLACK_BOT_TOKEN`.

```sh
SLACK_BOT_TOKEN=xoxb-your-token
```

## Invite the Bot to Channels

With `chat:write.public`, notifycat can post to public channels without a prior
invite. For private channels, invite the bot to every channel used by
`notifycat-mapping`:

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
