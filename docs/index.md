---
template: home.html
hide:
  - navigation
  - toc
---

<div class="nc-lifecycle">
  <div class="nc-lifecycle__stage">
    <div class="nc-lifecycle__label">1 · OPENED</div>
    <div class="nc-lifecycle__txt">🚀 please review <span class="nc-lifecycle__link">PR #169</span></div>
  </div>
  <div class="nc-lifecycle__arrow">→</div>
  <div class="nc-lifecycle__stage">
    <div class="nc-lifecycle__label">2 · IN REVIEW</div>
    <div class="nc-lifecycle__txt">🚀 <span class="nc-lifecycle__mention">@channel</span>, please review <span class="nc-lifecycle__link">PR #169</span></div>
    <div class="nc-lifecycle__ctx">mptooling/notifycat · opened Today at 8:30 PM</div>
    <div class="nc-lifecycle__txt">👁️ @Pavel reviewing · since 8:41 PM</div>
    <span class="nc-lifecycle__btn">Start review</span>
  </div>
  <div class="nc-lifecycle__arrow">→</div>
  <div class="nc-lifecycle__stage">
    <div class="nc-lifecycle__label">3 · MERGED</div>
    <div class="nc-lifecycle__txt"><s>[Merged] <span class="nc-lifecycle__link">PR #169</span></s></div>
    <div class="nc-lifecycle__txt">👁️ reviewed by @Pavel</div>
    <div><span class="nc-rx">✅ 2</span><span class="nc-rx">🎉 1</span></div>
  </div>
  <div class="nc-lifecycle__caption">the same message, updating in place</div>
</div>

Your channel becomes a status board, not an event log. Anyone can see where every PR stands at a glance, without scrolling through five notifications to work out whether something still needs eyes.

## Why teams run it

<div class="grid cards" markdown>

- :material-bell-off-outline: **Quiet**

    State changes become message updates and emoji reactions, not new posts. Dependabot bumps collapse to a single compact line. A busy repository produces one Slack line per PR — total.

- :material-eye-check-outline: **Nothing slips through**

    A morning digest resurfaces open PRs that nobody touched yesterday. The "Start review" button shows who is already reviewing. See [What you see in Slack](features.md) for the full tour.

- :material-package-variant-closed: **Easy to own**

    One Go binary, one declarative `config.yaml`, one SQLite file. The whole configuration is validated against Slack and your git host *before boot*. Two secrets, no GitHub App, no OAuth.

</div>

## The problem it solves

The usual way to connect pull requests to Slack is the official GitHub app: `/github subscribe owner/repo` plus `pulls`, `reviews`, and `comments`. It works, but every event becomes another Slack item. The events are all there; the *current state* is nowhere.

Notifycat inverts that. Your git host sends PR webhooks, Notifycat routes each repository to the right channel, and one PR keeps one message. Reviews and comments land on it as reactions; merge strikes it through.

<div class="nc-diagram-wrap">
--8<-- "docs/assets/images/diagrams/event-log-vs-status-board.svg"
</div>

## When it's not the fit

Notifycat is deliberately narrow. Pick something else if:

- **You want the full event stream in Slack.** Every review and comment as its own post is exactly what the official GitHub app does well.
- **You need GitHub and Bitbucket in one place.** A deployment serves one git host; covering both means two instances, each with its own configuration and database.
- **You need more than pull requests.** Issues, deployments, CI status — out of scope by design.
- **You post to more than one Slack workspace.** One deployment carries one bot token, so it posts to one workspace.

## Git Provider Support

| Feature | GitHub | Bitbucket |
| --- | --- | --- |
| Webhook signature verification (HMAC-SHA256) | Yes | Yes |
| Per-path / monorepo routing | Yes (needs `GITHUB_TOKEN`) | Yes (needs `BITBUCKET_TOKEN`) |
| Stuck-PR digest | Yes | Yes |
| Reactions & review flow | Yes | Yes |
| Token auth | Fine-grained PAT (Bearer) | Access token (Bearer) or scoped Atlassian API token (Basic) |

## Where next

<div class="grid cards" markdown>

- **[What you see in Slack](features.md)** — the message lifecycle, reactions, digest, and Start review button
- **[Install with Docker Compose](compose.md)** — running in ~10 minutes
- **[Configuration basics](configure.md)** — the whole model in two minutes
- **[Route repositories to channels](routing.md)** — mappings, mentions, reactions
- **[Troubleshooting](troubleshooting.md)** — fix a delivery that didn't reach Slack
- **[config.yaml reference](configuration.md)** — every key

</div>
