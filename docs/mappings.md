# Mappings

Notifycat reads its repository → Slack-channel routing from a single declarative YAML file. Edit the file in your
repository (or wherever your deployment mounts it), commit it, and `notifycat-server` picks it up on the next restart.

The full schema and a runnable starting point live in
[`mappings.example.yaml`](https://github.com/mptooling/notifycat/blob/main/mappings.example.yaml) at the repo root. Copy
it and edit it.

## File Location

| Env var | Default | Notes |
| --- | --- | --- |
| `NOTIFYCAT_MAPPINGS_FILE` | `./mappings.yaml` | Path to the YAML. Used by both `notifycat-server` and `notifycat-mapping`. |

The sibling **lock file** is derived from the YAML path: `.yaml` / `.yml` is replaced with `.lock`. So `./mappings.yaml`
produces `./mappings.lock`, and `/etc/notifycat/m.yaml` produces `/etc/notifycat/m.lock`.

## Schema

```yaml
mappings:
  <org>:                       # GitHub org name; map key
    channel: <slack-channel-id>
    mentions: [<string>, ...]  # optional; see "Mention states" below
    repositories: <"*" | [<repo>, ...]>
```

### Rules

| Field | Rule |
| --- | --- |
| `mappings` | Map keyed by GitHub org. Org keys match `^[A-Za-z0-9_.-]+$`. |
| `channel` | Required. Slack channel ID, matches `^[CGD][A-Z0-9]{2,}$` (must be the ID, not `#display-name`). |
| `mentions` | Optional. See [Mention states](#mention-states) for the three accepted shapes. `null` is rejected so the operator's intent stays explicit. |
| `repositories` | Required. Either the literal string `"*"` (every repo in the org) or a non-empty list of bare repo names matching `^[A-Za-z0-9_.-]+$`. Names cannot contain `/`. |
| `repositories: ["*", ...]` | Rejected. `"*"` is exclusive of named entries. |
| Duplicate repo within an org | Rejected at parse time. |
| Unknown keys anywhere | Rejected at parse time. Typos surface immediately. |

### Mention states

`mentions:` has three accepted shapes; pick the one that matches operator intent for that org.

| YAML | Slack message prefix | Meaning |
| --- | --- | --- |
| key omitted | `<!channel> ` (renders as `@channel`) | Broadcast to everyone in the channel. Default for entries that don't opt out. |
| `mentions: []` | _(no prefix; message starts with `please review …`)_ | Post silently — no ping. |
| `mentions: ["<@U…>", "<!subteam^S…>"]` | `<@U…>,<!subteam^S…>, ` | Ping the listed handles. |
| `mentions: null` / `mentions: ~` | _rejected at parse time_ | Ambiguous. Omit the key for `@channel`, or use `[]` for no ping. |

The absent-vs-`[]` distinction flows through `Provider.Get` and `Provider.Entries`: the absent state is materialized as
`Mentions: ["<!channel>"]` so downstream consumers (composer, list CLI) see a uniform slice. Entry hashes ignore
mentions entirely, so toggling between absent and `[]` does **not** invalidate the validation cache.

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

## Lookup Rules

When a webhook arrives for `org/repo`, the provider resolves the mapping in this order:

1. **Exact** match against `mappings.<org>.repositories[*]`.
2. **Wildcard** match if `mappings.<org>.repositories: "*"`.
3. **Miss** → no Slack message is posted for that PR.

`"*"` expansion is **lookup-time** and **in-memory** — the server never hits the GitHub API at webhook time, even for
wildcard orgs.

## CLI

The `notifycat-mapping` binary has two subcommands:

```sh
notifycat-mapping list                    # print the file as parsed (no network)
notifycat-mapping validate                # validate every entry, cache-aware
notifycat-mapping validate <owner/repo>   # validate one entry, ignore cache for it
notifycat-mapping validate --force        # validate every entry, ignore the cache entirely
```

### `list`

Prints a tab-aligned table of every parsed entry (org, repo, channel, mentions). Wildcards render as `*`. No network
calls. Useful as a sanity check after editing.

### `validate`

Runs end-to-end checks per entry: the channel ID is well-formed, the bot has the required Slack scopes and is a member
of the channel, and (when `GITHUB_TOKEN` is set) the GitHub webhook on each repo subscribes to the events Notifycat
needs. See [Operations](operations.md#validating-a-mapping) for the full check list and remediation table.

**Full mode** (`validate` with no positional arg) consults `mappings.lock` and only validates entries whose hash differs
— perfect for steady-state deploy boots. **Targeted mode** (`validate <owner/repo>`) always validates the one entry.
**`--force`** ignores the lock and revalidates every entry.

After a successful run, successful entries are merged into `mappings.lock`. Failed entries keep their old hash so a
transient outage doesn't invalidate the rest.

## Server Behavior at Startup

`notifycat-server` runs the same cache-aware validation on boot. If any entry fails, the server refuses to start and
exits non-zero — startup failures are visible immediately, not after a webhook arrives. An empty `mappings.yaml` (no
entries) skips validation and boots normally.

## Lock File

`mappings.lock` is a JSON cache of the SHA256 hash of each validated entry. The hash covers `(org, repo, channel)` only
— **mentions are excluded** so editing a `@`-handle doesn't bust the cache.

Commit the lock alongside `mappings.yaml` **in the operations repository that owns your deployment** — wherever you keep
your real `mappings.yaml`. The Notifycat source tree gitignores both files (see `CONTRIBUTING.md`); they are operator
state, not project state. On every boot, the server re-hashes the parsed entries; if every hash matches the lock, the
server boots without contacting Slack or GitHub. If any hash differs (or is new), only those entries are validated, and
the lock is updated.

Deletes are handled the same way — entries removed from the YAML drop out of the lock on the next successful write.

The lock has a comment field that warns operators not to edit by hand. Tampering only hurts the operator: faking hashes
only changes whether the server re-validates, not whether the mapping actually works.

## Operator Workflow

```
edit mappings.yaml
  → notifycat-mapping validate
  → commit both mappings.yaml and mappings.lock
  → deploy
```

If you skip the validate step, the server runs validation itself on the next boot. It's just slower (one round-trip per
changed entry) and the failure surfaces at deploy time instead of at edit time.

## Common Operations

### Add a repo to an existing org

Add the repo name to that org's `repositories` list and revalidate:

```sh
notifycat-mapping validate
```

### Add a new org

Append a new top-level map entry under `mappings:` and revalidate.

### Remove a repo

Delete its entry from the YAML. The lock cleans itself up on the next successful validate or server boot.

### Switch an org to wildcard

Replace the list with the string `"*"`:

```yaml
acme:
  channel: C0123ABCDE
  mentions: ["<@U0123ALICE>"]
  repositories: "*"
```

Then `notifycat-mapping validate` — wildcard expansion needs `GITHUB_TOKEN` to enumerate the org's repos. Without it,
wildcard entries report as `SKIP` and the server still routes them at webhook time.

### Move a repo between orgs

Remove it from the old org's list, add it to the new org's list, revalidate.
