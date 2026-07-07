# Reactions & bot reviews

Reactions are how Notifycat stays quiet: review activity lands as emoji on the existing PR message instead of new posts. This page covers changing the emoji, deciding what bot reviewers get to do, and the compact Dependabot format.

## Customizing reactions

The defaults live under `slack.reactions` in `config.yaml`:

```yaml
slack:
  reactions:
    enabled: true
    new_pr: eyes
    approved: white_check_mark
    commented: speech_balloon
    request_change: exclamation
    merged_pr: twisted_rightwards_arrows
    closed_pr: x
    bot_review: robot_face
```

Use Slack emoji names without colons — `approved: shipit`, not `:shipit:`. Custom workspace emoji work. `enabled: false` turns all reaction updates off while keeping the PR messages themselves.

Any of these keys can also be set per repository tier or per org `"*"` tier in `mappings:`, with the most specific level winning — one team's `approved: shipit` doesn't leak into the org. See [behavioral overrides](mappings.md#behavioral-overrides) for the full key list.

## Bot reviewers

Copilot reviews, dependabot auto-approvals, CI bots — anything the git host reports as a bot triggers the same review events humans do. Notifycat gives you two policies over that signal, and they're mutually exclusive:

**Mark (the default).** Bot reviews react normally **and** get the `bot_review` marker (:robot_face:) alongside. A bot's ✅ and a human's ✅ never look the same, but nothing is hidden. Set `slack.reactions.bot_review: ""` to drop the marker and let bot reviews blend in.

**Suppress.** With `reviews.ignore_ai_reviews: true`, bot review events add no reaction at all. The PR message stays clean of bot activity, and suppressed reviews deliberately don't count as "activity" for the [digest](digest.md) — an AI-only review pass never hides a PR that still needs a human.

```yaml
reviews:
  ignore_ai_reviews: true   # default false
```

### The trade-off, stated plainly

The git host's payload does not distinguish AI reviewers from scripted bots — a Copilot review and a `github-actions[bot]` auto-approve carry the same `Bot` sender type, and Notifycat deliberately keeps no allowlist of AI vendors (those bots get renamed faster than a list can rot). Suppression is therefore a uniform rule: **every** non-human reviewer goes silent. If your team wants its CI bot's green checkmark visible in Slack, leave suppression off and rely on the marker.

Two scope notes:

- Suppression affects reactions only. A bot-*authored* PR (dependabot opening a bump) still posts its message — that's [handled separately](#dependabot-and-renovate) below.
- On Bitbucket, bot detection keys on `actor.type != "user"`. A bot that authenticates as an ordinary Bitbucket **user account** looks human and won't be suppressed — prefer app-based integrations for reviewers you want silenced. Details: [Bitbucket behavior notes](configuration.md#bitbucket-behavior-notes).

Expected a reaction that never came? The suppression log line and the checklist are in [Troubleshooting → No reaction on a review](troubleshooting.md#no-reaction-on-a-review).

## Dependabot and Renovate

PRs **opened by** `dependabot[bot]` or `renovate[bot]` skip the "please review" format and post one compact line (on by default):

- `:package: <bot> bumped <PR link>` — routine bump
- `:rotating_light: <bot> security update <PR link>` — the PR body carries a security advisory

```yaml
reviews:
  dependabot_format: true   # set false for the standard format
```

Operator-relevant details:

- **Detection is by author login, exactly.** `dependabot[bot]` and `renovate[bot]`, case-insensitive, no prefix matching — a self-hosted Renovate with a custom bot login posts the standard format.
- **The security split fails safe.** Notifycat looks for a vulnerability advisory header in the PR body. A parse miss falls back to :package:, never a false :rotating_light: — so a future Dependabot template change can under-alert but never cry wolf.
- **Mentions are unchanged.** Bot PRs ping whatever the mapping pings. If your entry pings `@channel`, so do the bumps — consider `mentions: []` on dependency-heavy repositories.
