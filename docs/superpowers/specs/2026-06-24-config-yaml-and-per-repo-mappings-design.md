# Single `config.yaml` + per-repo mappings

## Summary

Two coupled changes to how Notifycat is configured:

1. **Consolidate non-secret configuration into a single `config.yaml`.** Today configuration is split between ~17 environment variables (non-secret app settings) and `mappings.yaml` (routing), with secrets also living in env. After this change, `config.yaml` is the only source for all non-secret configuration — app settings, routing, and the digest. `.env` shrinks to secrets plus the infra values that `docker-compose`/Caddy must interpolate.

2. **Make mapping configuration per-repository, with full override.** Today an org owns one channel + mentions shared across a flat list of repos, and the behavioral toggles are global. After this change, configuration is keyed per `org/repo` (with `org/*` as the org-wide tier), and any per-repo-meaningful setting can be overridden at the repo level, resolved by deep per-key merge over `global → org/* → org/repo`.

This is a deliberate breaking change with no automated migration. It bumps the minor version (pre-1.0: `0.16` → `0.17`).

## Motivation

- Configuration is scattered across env vars and a separate mappings file, with no clean secret/config boundary. A single declarative file is easier to read, diff, review, and reason about, and matches the project's existing preference for declarative YAML over imperative/env state.
- The per-org routing model can't express "repo A in this org goes to a different channel than repo B," nor "different reactions/digest behavior per repo." Real orgs need per-repo routing and behavior.

## Decisions (resolved during brainstorming)

- **Mappings fold into `config.yaml`.** `mappings.yaml` and `mappings.lock` as standalone files are retired.
- **No env override for non-secret config.** `config.yaml` is the only source. There is no precedence rule to document for app settings.
- **Secrets stay in env, read by fixed name.** `config.yaml` never references secrets. `.env` (dev) / process env (prod) carries `SLACK_BOT_TOKEN`, `GITHUB_WEBHOOK_SECRET`, `GITHUB_TOKEN`.
- **`.env` also carries infra-interpolation values.** `DOMAIN` and `ACME_EMAIL` are not secrets but are interpolated by `docker-compose` into Caddy, which cannot read `config.yaml`. They live in `.env`. `DOMAIN` is duplicated: the Go app reads it from `config.yaml` (`server.domain`, used by `notifycat-doctor` to derive the webhook URL); compose reads its own `DOMAIN` from `.env`. Accepted duplication.
- **Validation cache retained as `config.lock`.** Same mechanism as `mappings.lock`, retargeted. Gitignored, derived.
- **CLI renamed `notifycat-mapping` → `notifycat-config`.** Subcommands `list` and `validate`, both read-only against `config.yaml`.
- **Manual migration only.** A documented guide; no converter tool.
- **Per-repo override applies to all per-repo-meaningful settings.** Process/infra settings remain global-only.
- **Deep per-key merge, most-specific wins**, over the three tiers.

## File model

| File | Holds | Tracked |
| --- | --- | --- |
| `config.yaml` | All non-secret config: app settings + `mappings:` (per-repo) + global `digest:` defaults | gitignored (operator state); `config.example.yaml` committed |
| `.env` | Secrets (`SLACK_BOT_TOKEN`, `GITHUB_WEBHOOK_SECRET`, `GITHUB_TOKEN`) + infra interpolation (`DOMAIN`, `ACME_EMAIL`) | gitignored; `.env.example` committed |
| `config.lock` | Per-entry validation cache, keyed on resolved-entry hashes | gitignored (derived) |

`config.yaml` is discovered at `./config.yaml` by default, overridable via `NOTIFYCAT_CONFIG_FILE` — a bootstrap *locator*, not app data, and the only permitted env knob for non-secret behavior. Docker image default: `/app/config.yaml`.

## `config.yaml` schema

Top-level sections are the **global defaults**. The `mappings:` section holds the per-repo tiers.

```yaml
server:                       # global-only (one per process)
  addr: ":8080"
  log_level: info             # debug|info|warn|error
  log_format: text            # text|json
  domain: notifycat.example.com   # doctor derives https://$domain/webhook/github

database:                     # global-only
  url: "file:./data/notifycat.db"

slack:
  base_url: "https://slack.com"     # global-only (test/override)
  reactions:                  # per-repo overridable
    enabled: true
    new_pr: eyes
    merged_pr: twisted_rightwards_arrows
    closed_pr: x
    approved: white_check_mark
    commented: speech_balloon
    request_change: exclamation
    bot_review: robot_face

github:
  base_url: "https://api.github.com"  # global-only (GHE/test override)

cleanup:
  message_ttl_days: 30        # global-only (single DB, single cleanup loop)

reviews:                      # per-repo overridable
  ignore_ai_reviews: false
  dependabot_format: true

digest:                       # per-repo overridable (enabled + schedule)
  enabled: true
  schedule: "0 9 * * *"

mappings:
  acme:
    "*":                      # org-level tier: applies to every acme repo
      channel: C0ACMEDEFLT
      mentions: ["<!subteam^S0PLAT>"]
    api:                      # overrides only what it names; inherits the rest
      channel: C0APITEAM
      reviews: { dependabot_format: false }
    web:
      digest: { schedule: "0 8 * * 1-5" }
```

### Per-repo overridable vs global-only

- **Per-repo overridable:** `channel`, `mentions`, `slack.reactions.*`, `reviews.ignore_ai_reviews`, `reviews.dependabot_format`, `digest.enabled`, `digest.schedule`.
- **Global-only by nature** (one per process, can't vary per repo): `server.*`, `database.url`, `slack.base_url`, `github.base_url`, `cleanup.message_ttl_days`.

### Mappings structure

Every key under an org is a repo name or the literal `*`. The old `repositories: [...]` / `repositories: "*"` form is removed — repos are keys now. `channel` and `mentions` carry no global default; they must be provided by `org/repo` or `org/*`.

## Resolution

For a webhook from `acme/api`, the effective config is the deep per-key merge:

```
global (config.yaml top-level)  ⊕  acme/*  ⊕  acme/api
```

with the most-specific tier that sets a key winning. Lookup picks `acme/api` if that key exists, otherwise `acme/*`; if neither exists the event is ignored with `reason=no_mapping` (unchanged behavior).

### Presence tracking

Merge must distinguish "this tier set X" from "X is its zero value." Each per-tier mapping entry therefore decodes into a struct of **optional fields** (pointers or explicit presence flags), not plain values. The resolver folds the tiers, then applies final defaults. Two existing default behaviors consequently move from decode-time to **post-merge, resolved-level**:

- **`mentions` tri-state.** Absent at a tier now means *inherit*. The `@channel` fallback (`<!channel>`) fires only when **no** tier set `mentions` at all — this remains the global default. `mentions: []` still means "ping nobody"; explicit `mentions: null` / `~` is still rejected as ambiguous.
- **`digest.enabled` default-true.** Applies only when no tier set it.

### Config errors caught at parse/validate

- A resolvable repo (an explicit `org/repo`, or any repo matched by `org/*`) whose merged config yields no `channel` is a configuration error, surfaced at parse/validate, not at runtime.
- Unknown keys anywhere are rejected (typo safety), preserving today's strict parsing.

## Go restructure

`internal/config` owns the `config.yaml` schema and reuses `internal/mappings` for the mapping/digest domain types, keeping `internal/mappings` the single owner of mapping parsing, resolution, and lock logic.

- `config.Config` becomes a YAML-tagged struct for the global sections. It embeds the mappings tree (`map[string]Org`, where `Org` is now `map[string]RepoConfig` with a `*` key) and the global `Digest` defaults, reusing the mappings package's custom unmarshalers (strict unknown-key rejection; the `mentions` tri-state; presence tracking).
- `config.Load()`:
  1. `godotenv.Load()` — dev convenience, **secrets + infra only** now.
  2. Decode `config.yaml` with `yaml.KnownFields(true)` **over a defaults-initialized struct** (yaml.v3 leaves absent keys untouched, replacing every former `envDefault`).
  3. Read the three `Secret` fields from env by name.
  4. Validate (`message_ttl_days > 0`, every resolvable repo yields a channel, cron specs parse, etc.).
- `internal/mappings` gains:
  - A `RepoConfig` per-tier type with optional fields + presence tracking.
  - A resolver: `Resolve(global, orgStar, repo) Effective` performing the deep per-key merge and applying final defaults.
  - A `NewProvider(...)` constructor built from the already-decoded sections (replacing `mappings.Load(path)`'s file read). `Provider.Get` resolves and returns the effective per-repo config; `Provider.Entries` enumerates per-`org/repo` (and `org/*`) entries.
- Lock: `LockPath` retargets `config.yaml` → `config.lock`. Entries are per `org/repo`/`org/*`. Because the lock keys on per-entry hashes, changing a global app setting (e.g. `log_level`) does not invalidate it — only mapping edits do.

Downstream consumers (`app.Wire`, the PR handlers, the digest reporter) consume `Provider.Get`'s effective config and the global `Config` fields, so their wiring changes only where they must now ask for a *resolved* per-repo value (reactions, review toggles) rather than a global one.

## Digest scheduler

`internal/digest` changes from one global schedule to N. The scheduler collects the **set of distinct effective schedules** across all resolved repos, registers one cron per distinct spec, and on each tick includes the repos whose effective `digest.enabled == true && schedule == thisSpec`, grouped by resolved channel. A channel whose repos disagree on schedule posts at multiple times — an accepted consequence of full per-repo override.

## CLI rename

`cmd/notifycat-mapping` → `cmd/notifycat-config`. `notifycat-config list` prints the parsed config (mappings resolved per entry); `notifycat-config validate [owner/repo] [--force]` runs the cache-aware validation pipeline against `config.yaml`. Both read-only. Update `Dockerfile`, `justfile`, `mkdocs.yml`, and docs. All other binaries keep their names.

## Migration (manual, breaking — 0.17)

A new `docs/0.17-config-migration.md` covering:

- **Env → `config.yaml` table.** Every retired env var mapped to its `config.yaml` path, plus the explicit "stays in `.env`" list (secrets + `DOMAIN`/`ACME_EMAIL`).
- **Mappings restructure.** `org → {channel, mentions, repositories:[...]}` becomes `org → { repo|*: {channel, mentions, ...} }`. The common whole-org case converts to a single `org/*` entry. Mechanical, but every operator does it by hand.
- **Behavior change callout.** `mentions` absent now means *inherit*, not *@channel*; the `@channel` default only survives when no tier sets mentions.

Supporting changes: add `config.example.yaml`; retire `mappings.example.yaml` (fold its commentary into the new example); trim `.env.example` to secrets + infra. `config.Load()` fails fast with a clear, guide-pointing message if `config.yaml` is missing or if a now-removed env var (e.g. `LOG_LEVEL`) is still set.

## Testing

- `internal/config`: table-driven decode tests — defaults applied, unknown-key rejection, secrets-from-env, `message_ttl_days` guard, missing-`config.yaml` error, removed-env-var detection.
- `internal/mappings`: resolver tests for the deep per-key merge across all three tiers (each overridable key independently), the `mentions` inherit/`@channel`-final-default semantics, `digest.enabled` default-on after merge, the no-channel config error, and `org/*`-vs-`org/repo` precedence. `NewProvider` constructor and adapted parse/lock tests.
- `internal/digest`: scheduler with multiple distinct schedules — partition by effective schedule, per-tick channel grouping, a channel appearing in multiple ticks, a repo opting out via `digest.enabled=false`.
- `internal/app`: integration test swaps the two-file fixture for one `config.yaml`; assert end-to-end resolution for a repo overriding a subset of keys.

## Out of scope

- Automated config migration / conversion tooling.
- Per-repo override of process/infra settings (`server`, `database`, base URLs, cleanup).
- Hot-reload of `config.yaml` (still read at boot/CLI invocation).
