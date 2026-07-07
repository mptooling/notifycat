# Configuration basics

Everything Notifycat does is driven by two files, and only two:

| File | Holds | Read by |
| --- | --- | --- |
| `.env` | Secrets — two required values | The process environment |
| `config.yaml` | Everything else — provider, routing, tuning | The server and every CLI binary |

No admin UI, no settings API, no configuration in the database. If you can edit two text files, you can operate Notifycat.

## The minimal config

This is a complete, production-ready `config.yaml`:

```yaml
git_provider: github          # or: bitbucket

mappings:
  acme:                       # your GitHub org (or Bitbucket workspace slug)
    api:                      # a repository
      channel: C0123ABCDE     # the Slack channel ID — not "#name"
```

And the matching `.env`:

```sh
GITHUB_WEBHOOK_SECRET=your-generated-secret   # BITBUCKET_WEBHOOK_SECRET for bitbucket
SLACK_BOT_TOKEN=xoxb-your-slack-bot-token
```

That's it. PRs from `acme/api` post to that channel, updates land on the same message, the stuck-PR digest fires at 9am UTC, and the default reaction emoji apply. Every other setting is fine-tuning you can add when a real need shows up.

The defaults are deliberately opinionated: the digest is **on**, reactions are **on**, and a mapping without `mentions:` pings `@channel` — loud on purpose, so a fresh install never drops a PR silently. Tighten each of those once routing works.

## The change loop

Every configuration change follows the same three steps:

```sh
$EDITOR config.yaml
notifycat-config validate     # checks each entry against Slack (and the git host)
docker compose restart notifycat
```

`validate` confirms the channel exists, the bot token has the right scopes, the bot is in the channel, and — with a read token — that the webhook covers the right events. Skip it and nothing breaks: the server runs the same validation at boot and **refuses to start** on a failing entry, so a typo'd channel ID surfaces at deploy time, not when the first webhook arrives. Details: [validate and deploy a change](routing.md#validate-and-deploy-a-change).

## The fine-tuning map

When you outgrow the minimal config, each need has its own page:

<div class="grid cards" markdown>

- **[Route repositories to channels](routing.md)** — route more repositories, catch a whole org, tune who gets pinged
- **[Monorepos](monorepo.md)** — route a monorepo by the directories a PR touches
- **[Reactions & bot reviews](bots-and-reactions.md)** — change the reaction emoji, mark or mute bot reviews, tune the Dependabot format
- **[Stuck-PR digest](digest.md)** — change the digest schedule or timezone, or turn it off
- **[config.yaml reference](configuration.md)** — look up any key, its default, and its exact rules
- **[Mappings schema](mappings.md)** — the exact `mappings:` schema and inheritance rules

</div>

Behavioral settings (reactions, reviews, digest) can also be overridden per repository or per org — most specific wins. The override list lives in the [mappings reference](mappings.md#behavioral-overrides).
