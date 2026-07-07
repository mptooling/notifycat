# Mappings schema reference

The exact rules for the `mappings:` section of `config.yaml`. For task-oriented guidance — pointing repositories at channels, mentions, common edits — see [Route repositories to channels](routing.md); for path-routing behavior, [Monorepos](monorepo.md).

## File location

`mappings:` is a top-level section of `config.yaml` (default path `./config.yaml`, overridable via `NOTIFYCAT_CONFIG_FILE`). The sibling **lock file** `config.lock` is written next to it and is operator-derived state — gitignored in this repository, committed in the ops repository that owns your deployment.

A runnable starting point lives in [`config.example.yaml`](https://github.com/mptooling/notifycat/blob/main/config.example.yaml).

## Schema

```yaml
mappings:
  <org>:                          # GitHub org name (or Bitbucket workspace slug); map key
    <repo>:                       # repository name (or Bitbucket repo_slug), or "*" for catch-all
      channel: <slack-channel-id>
      mentions: [<string>, ...]   # optional; tri-state, see below
    "*":                          # optional catch-all tier; also supplies defaults
      channel: <slack-channel-id>
      mentions: [<string>, ...]
```

<a id="bitbucket-workspace-and-repository-slug"></a>

Under `git_provider: bitbucket` the org key is the Bitbucket **workspace slug** and the repository key is the **repository slug** — the lowercase hyphenated URL identifier, not the display name. Both appear in any repository URL: `bitbucket.org/<workspace>/<repo_slug>`. The schema is identical across providers; only the key semantics differ.

### Rules

| Field | Rule |
| --- | --- |
| `mappings` | Map keyed by org / workspace slug. Keys match `^[A-Za-z0-9_.-]+$`. |
| `<org>.<repo>` | Repository names / slugs matching `^[A-Za-z0-9_.-]+$`, or the literal `"*"`. |
| `channel` | Slack channel ID matching `^[CGD][A-Z0-9]{2,}$` — the ID, never `#display-name`. Omitted on a repository tier → inherited from `"*"`. Every resolvable org/repository pair must yield a channel. |
| `mentions` | Optional tri-state (below). `null` is rejected. Omitted on a repository tier → inherited from `"*"`. |
| `"*"` tier | Optional. Supplies channel/mentions defaults for repository tiers, and catches any webhook for an unlisted repository in the org. An org may be defined by `"*"` alone. |
| Duplicate repository within an org | Rejected at parse time. |
| Unknown keys anywhere | Rejected at parse time — typos surface immediately. |

### Resolution

For a webhook targeting `org/repo`:

1. An explicit `org/<repo>` tier wins for every key it sets.
2. Keys it doesn't set fall back to the `org/*` tier.
3. No tier matches at all → no Slack message (logged as `no_mapping`).
4. `channel` unresolvable after both tiers → the org is malformed, rejected at parse time.
5. `mentions` unresolvable → falls back to `<!channel>`.

Resolution is lookup-time and in-memory — the server never calls the git host's API to route a plain mapping, even for wildcard-only orgs.

### Mention states

| YAML | Slack message prefix | Meaning |
| --- | --- | --- |
| key omitted | inherited; final fallback `<!channel>` | Broadcast to the channel |
| `mentions: []` | *(none — message starts with "please review …")* | Post silently |
| `mentions: ["<@U…>", "<!subteam^S…>"]` | the handles, comma-joined | Ping exactly those |
| `mentions: null` / `~` | — | **Rejected at parse time** |

The absent state materializes as `["<!channel>"]` during inheritance resolution, so downstream consumers see a uniform slice. Entry hashes ignore mentions entirely — toggling between absent and `[]` does **not** invalidate the validation cache.

Wire formats (user `<@U…>`, group `<!subteam^S…>`, `<!channel>`, `<!here>`) are listed in [routing → Mentions](routing.md#mentions).

## Behavioral overrides

A repository tier (and the `"*"` tier) may override behavioral settings that otherwise come from the global config sections:

- **Reactions:** `reactions.enabled`, `reactions.new_pr`, `reactions.merged_pr`, `reactions.closed_pr`, `reactions.approved`, `reactions.commented`, `reactions.request_change`, `reactions.bot_review`
- **Reviews:** `reviews.ignore_ai_reviews`, `reviews.dependabot_format`
- **Digest:** `digest.enabled`, `digest.schedule` — but **not** `digest.timezone`, which is global only and rejected on a tier

Inheritance, most-specific wins: repository tier → org `"*"` tier → global section → built-in default. Not overridable per repository: `server.*`, `database.url`, `slack.base_url`, `github.base_url` / `bitbucket.base_url`, `cleanup.message_ttl_days`.

## Per-path routing (monorepos)

A **named** repository tier may add a `paths:` block; how PRs select channels at runtime is covered in [Monorepos](monorepo.md). A path entry accepts exactly two optional keys:

| Key | Rule |
| --- | --- |
| `channel` | Optional. Omitted → inherits the repository tier's channel. If set, matches `^[CGD][A-Z0-9]{2,}$`. |
| `mentions` | Optional. Same tri-state as a repository tier; `null` rejected. |

Parse-time validation — the server fails fast on any violation:

- `paths:` on the `"*"` tier is rejected (it would apply to every repository in the org).
- Directory keys are normalized: leading/trailing slashes stripped, path cleaned — `/config`, `config`, and `config/` are the same key, and two keys normalizing to the same directory are rejected as a collision.
- Keys are case-sensitive and must match the repository's real directory casing.
- Empty keys, the root `/`, and any key containing a `..` segment are rejected.
- Duplicate keys within a tier or path node are an error, never a silent last-wins.
- A repository with `paths:` must still resolve a base channel from its own tier or `"*"`, so an unmatched PR always has a destination.

## Lock file

`config.lock` is a JSON cache holding a SHA256 hash per validated entry, covering `(org, repo, channel)` — mentions are excluded, so editing a handle doesn't bust the cache. On boot the server re-hashes the parsed entries: all hashes match → boot without contacting Slack or the git host; any differ or are new → only those revalidate, and the lock is updated. Entries deleted from the YAML drop out of the lock on the next successful write.

The lock's comment field warns against hand-editing. Tampering only hurts the operator — faked hashes change whether entries revalidate, not whether they work.

## Startup behavior

`notifycat-server` runs the same cache-aware validation at boot. Any failing entry aborts startup with a non-zero exit and the failing details logged — misconfigurations surface at deploy time, not when the first webhook arrives. An empty `mappings:` section boots normally and routes nothing.
