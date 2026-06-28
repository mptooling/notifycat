# Mappings

> **0.18 and later:** Mappings use a per-repository-tier schema in the `mappings:` section of `config.yaml`. Each org contains named repo tiers and an optional `"*"` catch-all tier. See the [0.18 migration guide](0.18-per-repo-mappings-migration.md) if you are upgrading from 0.17.

Notifycat reads its repository → Slack-channel routing from the `mappings:` section of `config.yaml`. Edit that file in your deployment directory, and `notifycat-server` picks it up on the next restart.

A runnable starting point lives in [`config.example.yaml`](https://github.com/mptooling/notifycat/blob/main/config.example.yaml) at the repo root. Copy it and edit it.

## File Location

The `mappings:` section is part of `config.yaml`. The config file path defaults to `./config.yaml` and is overridable via `NOTIFYCAT_CONFIG_FILE`. See [Configuration](configuration.md) for details.

The sibling **lock file** is `config.lock`, derived by the same mechanism as the old `mappings.lock` — placed next to `config.yaml` and gitignored (operator-derived state).

## Schema

```yaml
mappings:
  <org>:                          # GitHub org name; map key
    <repo>:                       # GitHub repo name within the org, or "*" for catch-all
      channel: <slack-channel-id>
      mentions: [<string>, ...]   # optional; see "Mention states" below
    "*":                          # Optional catch-all tier (supplies defaults, routes unlisted repos)
      channel: <slack-channel-id>
      mentions: [<string>, ...]   # optional
```

### Rules

| Field | Rule |
| --- | --- |
| `mappings` | Map keyed by GitHub org. Org keys match `^[A-Za-z0-9_.-]+$`. |
| `<org>.<repo>` | Repo tier keys are GitHub repo names (matching `^[A-Za-z0-9_.-]+$`) or the literal string `"*"`. |
| `channel` | Required on at least one tier per org. Slack channel ID, matches `^[CGD][A-Z0-9]{2,}$` (must be the ID, not `#display-name`). If omitted on a repo tier, inherits from the `"*"` tier. Every resolvable org/repo pair must yield a channel. |
| `mentions` | Optional. See [Mention states](#mention-states) for the three accepted shapes. If omitted on a repo tier, inherits from the `"*"` tier. `null` is rejected. |
| `"*"` tier | Optional. Supplies channel/mentions defaults for repo tiers that omit them. Also acts as the catch-all: any webhook for `org/repo` without an explicit tier routes to `org/*`. An org may be wholly defined via `"*"` alone. |
| Duplicate repo within an org | Rejected at parse time. |
| Unknown keys anywhere | Rejected at parse time. Typos surface immediately. |

### Resolution

When a webhook arrives for `org/repo`:

1. Look for an explicit `org/<repo>` tier. If found and it sets a key (channel or mentions), use it.
2. Fall back to the `org/*` tier for any key not set by the repo tier. If `org/*` sets a key, use it.
3. If `channel` is still unset after both tiers, the org is malformed and rejected at parse time.
4. If `mentions` is still unset, fall back to `@channel` (<!channel>).

### Mention states

`mentions:` has three accepted shapes; pick the one that matches operator intent.

| YAML | Slack message prefix | Meaning |
| --- | --- | --- |
| key omitted | Inherits from parent tier; final fallback `<!channel> ` (renders as `@channel`) | Broadcast to everyone in the channel. |
| `mentions: []` | _(no prefix; message starts with `please review …`)_ | Post silently — no ping. |
| `mentions: ["<@U…>", "<!subteam^S…>"]` | `<@U…>,<!subteam^S…>, ` | Ping the listed handles. |
| `mentions: null` / `mentions: ~` | _rejected at parse time_ | Ambiguous. Omit the key to inherit, or use `[]` for no ping. |

The absent state is materialized as `Mentions: ["<!channel>"]` during inheritance resolution, so downstream consumers (composer, list CLI) see a uniform slice. Entry hashes ignore mentions entirely, so toggling between absent and `[]` does **not** invalidate the validation cache.

### Mention Formats

Mentions are arbitrary strings joined with `,` into the Slack message prefix. Use Slack's wire format so they actually
broadcast:

| Mention | Format |
| --- | --- |
| User | `<@U123456>` |
| User group / subteam | `<!subteam^S123456>` |
| Channel-wide broadcast | `<!channel>` |
| Online-only broadcast | `<!here>` |

In Slack, copy a user's member ID from their profile menu. For user-group IDs, use Slack admin tooling or inspect the
wire format of an existing group mention.

## Behavioral Overrides

Each repo tier can override behavioral settings: `reactions`, `reviews`, and `digest`. These settings inherit from the `org/*` tier (if present) and fall back to the global `config.yaml` section when omitted. For example, a repo can use a different approval reaction, suppress AI reviews differently, or post its digest on its own schedule. See the [0.18 migration guide](0.18-per-repo-mappings-migration.md#per-repo-behavioral-overrides) for the full list of overridable keys and the inheritance chain (most-specific tier wins).

## Per-path routing (monorepos)

A named repo tier may add a `paths:` block that routes a PR by the directories its changed files touch — so each team in a monorepo hears about the directories it owns. This section documents the **schema and validation**; how a PR's files select a path (most-specific wins, mentions union, single message per PR) is covered under [routing behavior](#per-path-resolution) and requires a GitHub token to read a PR's changed files.

```yaml
mappings:
  acme:
    the-monorepo:
      channel: C0MONO00000                 # base channel for PRs that match no path
      mentions: ["<!subteam^S0ENG>"]       # base ping for unmatched PRs
      paths:
        "/modules/acme":                   # directory key (see normalization below)
          mentions: ["<!subteam^S0TEAMA>"] # channel omitted → inherits C0MONO00000
        "/src/AuthBundle":
          channel: C0AUTH00000             # this directory also overrides the channel
          mentions: ["<!subteam^S0AUTH>"]
        "/vendor":
          mentions: []                     # matches the dir, pings nobody
```

A path entry accepts exactly two optional keys:

| Key | Rule |
| --- | --- |
| `channel` | Optional Slack channel ID. Omitted → inherits the repo tier's channel. If set, must match `^[CGD][A-Z0-9]{2,}$`. |
| `mentions` | Optional. Same tri-state as a repo tier: **absent** → inherit; `[]` → ping nobody; `["<@U…>", "<!subteam^S…>"]` → ping those; `["<!channel>"]` → explicit @channel. `mentions: null` is rejected. |

Validation rules (all enforced at parse time — the server fails fast):

- **Named tiers only.** `paths:` on the `"*"` org-default tier is rejected (it would apply to every repo in the org). Put path rules on a named repo tier.
- **Directory keys are normalized.** Leading/trailing slashes are stripped and the path is cleaned, so `/config`, `config`, and `config/` are equivalent. Keys are matched against repo-relative file paths (GitHub returns paths with no leading slash).
- **Keys are case-sensitive** — they must match the repository's real directory casing, exactly as GitHub reports it.
- **Rejected keys:** empty, root (`/`), or any key containing a `..` segment.
- **No collisions:** two keys that normalize to the same directory (e.g. `/config` and `config/`) are rejected.
- **No duplicate keys** within a tier or a path node — duplicates are an error, not a silent last-wins.
- **A base channel is still required.** A repo with `paths:` must still resolve a channel from its own tier or the org `"*"` tier, so a PR that matches no path always has a destination.

<a id="per-path-resolution"></a>
> **Routing behavior & worked example** — the rules for how multiple matched directories resolve to a single channel and a unioned mentions list, plus the token requirement and a worked example, are documented with the path-routing runtime (forthcoming). Until then, `paths:` is parsed and validated but not yet consulted at delivery time.

## Stuck-PR digest

Alongside the per-repo-tier `mappings`, an optional **global** `digest:` section configures a scheduled reminder that lists open PRs nobody has touched since the previous day. The digest can also be customized per-repo tier (see above). Both `digest:` and `mappings:` are top-level sections inside `config.yaml`.

```yaml
digest:
  enabled: true          # optional, default true — set false to turn the digest off
  schedule: "0 9 * * *"  # optional, default 9am daily (standard 5-field cron, in `timezone` below)
  timezone: "UTC"        # optional, default UTC — IANA zone the schedule + cutoff run in (global only)

mappings:
  acme:
    "*":
      channel: C0123ABCDE
```

| Field | Rule |
| --- | --- |
| `digest` | Optional. The feature is **on by default**: with no `digest:` section the server posts the digest at 9am daily, in UTC. |
| `digest.enabled` | Optional bool, default `true`. Set `false` to disable the digest entirely. |
| `digest.schedule` | Optional standard 5-field cron expression (e.g. `0 9 * * *`). Default `0 9 * * *`. Evaluated in `digest.timezone`. An invalid expression fails server startup. |
| `digest.timezone` | Optional IANA timezone name (e.g. `Europe/Kyiv`). Default `UTC`. Sets the zone for both the schedule and the "stuck since before today" cutoff. An unrecognized zone fails server startup. **Global only** — a per-repo `digest:` override that sets `timezone` is rejected at parse time. |
| Unknown keys under `digest` | Rejected at parse time, like the rest of the file. |

On each tick the server posts a parent message per Slack channel and lists that channel's stuck PRs in a single reply under it — open PRs whose last activity (the open notification, a review, or a comment) predates the start of the current day. Merged/closed PRs and PRs converted back to draft are excluded. The parent pings the same `mentions` as the channel's mapping, so it notifies the room; channels with no stuck PRs get no message. See [Operations → Stuck-PR digest](operations.md#stuck-pr-digest) for the runtime behavior and the upgrade note.

> **Enabled by default.** Upgrading from a release without this feature starts the 9am digest automatically. Add `digest: { enabled: false }` to keep the previous quiet behavior.

## Lookup Rules

When a webhook arrives for `org/repo`, the provider resolves the mapping in this order:

1. **Explicit tier** match: if `mappings.<org>.<repo>` exists, resolve against that tier (channel/mentions set there, else inherit from `org/*`).
2. **Catch-all tier** match: if `mappings.<org>."*"` exists and no explicit tier was found, resolve against the `"*"` tier.
3. **Miss** → no Slack message is posted for that PR.

Tier resolution is **lookup-time** and **in-memory** — the server never hits the GitHub API at webhook time, even for orgs with only a `"*"` tier. The explicit tier (if present) always takes precedence over the catch-all.

## CLI

The `notifycat-config` binary has two subcommands:

```sh
notifycat-config list                    # print the file as parsed (no network)
notifycat-config validate                # validate every entry, cache-aware
notifycat-config validate <owner/repo>   # validate one entry, ignore cache for it
notifycat-config validate --force        # validate every entry, ignore the cache entirely
```

### `list`

Prints a tab-aligned table of every parsed entry (org, repo, channel, mentions). Wildcards render as `*`. No network calls. Useful as a sanity check after editing.

### `validate`

Runs end-to-end checks per entry: the channel ID is well-formed, the bot has the required Slack scopes and is a member of the channel, and (when `GITHUB_TOKEN` is set) the GitHub webhook on each repo subscribes to the events Notifycat needs. See [Operations](operations.md#validating-a-mapping) for the full check list and remediation table.

**Full mode** (`validate` with no positional arg) consults `config.lock` and only validates entries whose hash differs — perfect for steady-state deploy boots. **Targeted mode** (`validate <owner/repo>`) always validates the one entry. **`--force`** ignores the lock and revalidates every entry.

After a successful run, successful entries are merged into `config.lock`. Failed entries keep their old hash so a transient outage doesn't invalidate the rest.

## Server Behavior at Startup

`notifycat-server` runs the same cache-aware validation on boot. If any entry fails, the server refuses to start and exits non-zero — startup failures are visible immediately, not after a webhook arrives. An empty `mappings:` section (no entries) skips validation and boots normally.

## Lock File

`config.lock` is a JSON cache of the SHA256 hash of each validated entry. The hash covers `(org, repo, channel)` only — **mentions are excluded** so editing a `@`-handle doesn't bust the cache.

Commit the lock alongside `config.yaml` **in the operations repository that owns your deployment**. The Notifycat source tree gitignores both files (see `CONTRIBUTING.md`); they are operator state, not project state. On every boot, the server re-hashes the parsed entries; if every hash matches the lock, the server boots without contacting Slack or GitHub. If any hash differs (or is new), only those entries are validated, and the lock is updated.

Deletes are handled the same way — entries removed from the YAML drop out of the lock on the next successful write.

The lock has a comment field that warns operators not to edit by hand. Tampering only hurts the operator: faking hashes only changes whether the server re-validates, not whether the mapping actually works.

## Operator Workflow

```
edit config.yaml
  → notifycat-config validate
  → commit both config.yaml and config.lock
  → deploy
```

If you skip the validate step, the server runs validation itself on the next boot. It's just slower (one round-trip per changed entry) and the failure surfaces at deploy time instead of at edit time.

## Common Operations

### Add a repo to an existing org

Add a new repo tier under that org's entry and set its channel (or omit to inherit from `"*"`):

```yaml
acme:
  api:
    channel: C0123ABCDE
  web:                    # new repo tier
    channel: C0456FGHIJ
  "*":
    channel: C0DEFAULT00
```

Then run `notifycat-config validate`.

### Add a new org

Append a new top-level map entry under `mappings:` with at least one tier that sets a channel, and revalidate.

### Remove a repo

Delete its tier from the YAML. The lock cleans itself up on the next successful validate or server boot.

### Switch an org to wildcard-only

Remove all explicit repo tiers and keep only the `"*"` tier:

```yaml
acme:
  "*":
    channel: C0123ABCDE
    mentions: ["<@U0123ALICE>"]
```

Then `notifycat-config validate`. The `"*"` tier catches every repo in the org.

### Move a repo between orgs

Remove the repo tier from the old org, add it to the new org's entry, revalidate.
