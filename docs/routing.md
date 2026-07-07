# Route repositories to channels

All routing lives in the `mappings:` section of `config.yaml`. Edit the file, validate it, restart the server — no database, no UI. This page covers the everyday tasks; the exact schema and matching rules are in the [mappings reference](mappings.md).

## Point a repository at a channel

```yaml
mappings:
  acme:                        # GitHub org (or Bitbucket workspace slug)
    api:                       # repository name (or Bitbucket repository slug)
      channel: C0123ABCDE      # the Slack channel ID — not "#name"
```

Use the channel **ID** (open the channel details in Slack and copy it — it starts with `C`). Display names like `#engineering` are rejected. On Bitbucket the org key is the workspace slug and the repository key is the URL slug (`my-service`, not `My Service`) — both visible in any repository URL.

## Catch a whole org

The `"*"` tier routes every repository you didn't list explicitly:

```yaml
mappings:
  acme:
    "*":
      channel: C0DEFAULT00
```

The `"*"` tier does double duty: it catches unlisted repositories, **and** it supplies defaults that named repository tiers inherit. A repository tier that omits `channel` or `mentions` takes them from `"*"`.

```yaml
mappings:
  acme:
    api:
      channel: C0API00000           # api gets its own channel, inherits "*" mentions
    web:
      mentions: ["<@U0WEBLEAD>"]    # web sets its own ping, inherits the "*" channel
    "*":
      channel: C0DEFAULT00
      mentions: ["<!subteam^S0ENG>"]
```

One consequence worth knowing: because `"*"` is also the catch-all, you can't build a shared-channel group that acts as an allowlist. If you want three repositories in one channel and everything else ignored, repeat the channel on each of the three tiers and leave `"*"` out.

!!! note "The webhook side of a catch-all"
    A `"*"` tier only routes deliveries that actually arrive — Notifycat can't announce a PR it never hears about. To have every repository in the org deliver (including ones created later) with a single registration, use an [organization-level webhook](github-webhook.md#organization-level-webhook); on Bitbucket, the equivalent is a workspace webhook. Per-repository webhooks work just as well — you just create one per repository, and again for each new repository.

## Mentions

`mentions:` controls who gets pinged in the PR message prefix. It has three deliberate states:

| YAML | Effect |
| --- | --- |
| key omitted | Inherit from the `"*"` tier; final fallback is `@channel` |
| `mentions: []` | Post silently — no ping at all |
| `mentions: ["<@U123456>", "<!subteam^S123456>"]` | Ping exactly those handles |

`mentions: null` is rejected at parse time — omit the key to inherit, or use `[]` for silence.

Use Slack's wire format so the pings actually fire:

| Mention | Format |
| --- | --- |
| User | `<@U123456>` |
| User group | `<!subteam^S123456>` |
| Everyone in the channel | `<!channel>` |
| Online members only | `<!here>` |

Copy a user's member ID from their Slack profile menu. For user-group IDs, inspect an existing group mention or use Slack's admin tooling.

!!! tip "Defaults are loud on purpose"
    With no `mentions:` anywhere, Notifycat pings `@channel` so a new install never drops a PR silently. Once routing works, tighten each mapping to a team handle — or `[]` for channels that only want the status board.

## Validate and deploy a change

The operator loop for any `config.yaml` edit:

```sh
notifycat-config validate     # checks every changed entry against Slack (and the git host)
# commit config.yaml + config.lock in your ops repository, then deploy
```

`validate` confirms each channel ID is well-formed, the bot token has the right scopes, the bot is a member of the channel, and — when a read token is set — that the repository's webhook subscribes to the events Notifycat needs. Successful results are cached in the sibling `config.lock`, so the next server boot revalidates only what changed.

Skip the step and nothing breaks: the server runs the same validation at boot and **refuses to start** if any entry fails. Validating at edit time just moves the failure to where you can see it. Full CLI detail: [CLI reference](cli.md#notifycat-config).

## Common operations

**Add a repository to an existing org** — add a tier under the org, set its channel (or omit it to inherit from `"*"`), revalidate.

**Add a new org** — add a top-level key under `mappings:` with at least one tier that resolves a channel, revalidate.

**Remove a repository** — delete its tier. The lock file cleans itself up on the next successful validate or boot.

**Move a repository between orgs** — delete the tier from one org, add it to the other, revalidate.

**Switch an org to wildcard-only** — remove the named tiers, keep `"*"`:

```yaml
acme:
  "*":
    channel: C0123ABCDE
    mentions: ["<@U0123ALICE>"]
```

## Beyond channels

A repository tier can also override behavior, not just destination — its own reaction emoji, its own bot-review policy, its own digest schedule. Same inheritance chain: repository tier → `"*"` tier → global config. See [Reactions & bot reviews](bots-and-reactions.md) and [Stuck-PR digest](digest.md), or the [full key list](mappings.md#behavioral-overrides).

For monorepos, a `paths:` block routes a PR by the directories it touches — one message per matched team channel. That's its own page: [Monorepos](monorepo.md).
