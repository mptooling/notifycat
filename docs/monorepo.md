# Monorepos: per-path routing

In a monorepo, "the right channel" depends on *which files* a PR touches. A `paths:` block on a repository tier routes each PR by the directories its changed files live under, so every team hears about the code it owns — and a cross-cutting PR fans out to one Slack message per matched channel.

```yaml
mappings:
  acme:
    the-monorepo:
      channel: C0MONO00000                 # base channel for PRs that match no path
      mentions: ["<!subteam^S0ENG>"]
      paths:
        "/modules/acme":
          mentions: ["<!subteam^S0TEAMA>"] # channel omitted → inherits C0MONO00000
        "/src/AuthBundle":
          channel: C0AUTH00000             # this directory gets its own channel
          mentions: ["<!subteam^S0AUTH>"]
        "/vendor":
          mentions: []                     # matched, but pings nobody
```

A path rule may also declare **multiple channels** using the `channels:` list form instead of a single `channel:` key. When a matched path rule uses `channels:`, those channels **replace** the repository's base channel set for that PR — the base channel is not included unless it appears in the list.

```yaml
mappings:
  acme:
    monorepo:
      channel: C0BASE
      paths:
        services/pay:
          channels:
            - channel: C0PAY1              # mentions absent → @channel
            - channel: C0PAY2
              mentions: []                 # ping nobody in this channel
```

A path entry may use `channel:` (+ optional `mentions:`), `channels:` (per-entry mentions), or neither (channel inherited from the repo tier). `channel:` and `channels:` are mutually exclusive on the same entry, and `channels:` may not appear alongside a path-level `mentions:` key.

## How a PR picks its channels

1. **Each changed file picks its most-specific rule.** A rule matches when the file lives under its directory, segment-aware — `modules/acme` matches `modules/acme/x.go` but not `modules/acmexyz/x.go`. When several rules match one file, the longest directory wins.
2. **Matched rules resolve to a channel set.** A rule with a single `channel:` resolves to that channel (or the repository's base channel if omitted). A rule with `channels:` resolves to all channels in the list, replacing the base set.
3. **One message per channel, mentions unioned.** Each channel gets a single message pinging the union of its rules' mentions, deduped.
4. **No match → the base tier.** A PR whose files match nothing posts one message to the repository's base channel set, exactly like a non-monorepo repository.

Later events — reviews, close, merge, draft — act on **every** message the PR fanned out to. The set of messages is fixed at announcement time; files pushed later don't add channels.

### Worked example

With the config above, a PR changing both `modules/acme/handler.go` and `src/AuthBundle/auth.go` produces **two** messages: one in `C0MONO00000` pinging `<!subteam^S0TEAMA>`, one in `C0AUTH00000` pinging `<!subteam^S0AUTH>`. An approval later adds the ✅ reaction to both. A PR touching only `README.md` matches nothing and posts once to `C0MONO00000` with the base mentions.

## Party settings per path

A path rule can also set `party_reviewers` and `party_deadline`. These keys control the [review party](agent-api.md#review-as-a-party) of the agent API. Slack ignores them. A path that sets only party keys never takes files away from a Slack rule.

```yaml
mappings:
  acme:
    protorepo:
      channel: C0PROTO000
      party_reviewers: 2
      paths:
        docs:
          party_reviewers: 1
        services:
          party_reviewers: 4
          party_deadline: 30m
        services/billing:
          party_deadline: 15m
```

The levels, most specific first, are the path, the repository tier, the org `"*"` tier, `agent_api`, and the built-in default (`1` and `1h`). Notifycat resolves each key for each changed file.

1. **The most specific path rule that sets the key wins.** A nested rule without the key takes the value of its parent rule.
2. **A file outside every path rule takes the repository's value.**
3. **The PR takes the highest cap and the shortest timer over its files.** This is per key.

Example 1: the repository cap is 2 and `docs` sets 1. A PR changes `docs/a.md` and `README.md`. The cap of the PR is 2.

Example 2: `services` sets 4 and `services/billing` sets no cap. A PR changes `services/billing/x.go`. The cap of the PR is 4. Its timer is 15 minutes.

### When notifycat reads the files

Notifycat reads the changed files only when a path rule of that repository sets a party key. The read has a limit of 2 seconds.

- Without a read token, the PR takes the repository's values. Notifycat logs nothing.
- When the read fails, a PR that is already stored keeps its cap and timer. A new PR takes the repository's values. Notifycat logs [`feed party settings fallback`](operations.md#logging).

A fallback never lowers a stored cap.

## The token requirement

Reading a PR's changed files takes an API call, so path routing needs a read token in `.env` — `GITHUB_TOKEN` on GitHub, `BITBUCKET_TOKEN` on Bitbucket ([scopes](bitbucket-webhook.md#access-token-scopes)). **Without one, path rules are inert**: every PR routes to the base tier as if `paths:` weren't there. This degradation is announced three ways — the [doctor](doctor.md) reports `SKIP` on its `path routing` check, `notifycat-config` prints a warning, and the server logs one at startup.

The token is the only API read the server ever performs at webhook time; plain (non-`paths`) routing is resolved entirely in memory.

## Validation

Path blocks are checked at parse time, and the server fails fast on any violation: directory keys are normalized (`/config`, `config`, `config/` are the same key — collisions rejected), keys are case-sensitive against the repository's real casing, `..` segments and the root `/` are rejected, and `paths:` is only allowed on named repository tiers — never on `"*"`. A repository with `paths:` still needs a resolvable base channel, so unmatched PRs always have a destination. The full rule table is in the [mappings reference](mappings.md#per-path-routing-monorepos).

## Digest interplay

A fanned-out PR appears in the digest of **each** channel it posted to, until it merges or closes. If that's too chatty for a low-traffic team channel, give that repository tier its own `digest: { enabled: false }` override — see [Stuck-PR digest](digest.md#per-repository-overrides).
