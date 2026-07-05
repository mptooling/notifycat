# Upgrading

Notifycat applies its database migrations automatically on server startup (see [Operations → Startup](operations.md#startup-and-shutdown)), so most upgrades are "pull the new image and restart." This page calls out the releases that need an operator action beyond that.

## `git_provider` is now required

Notifycat now requires a top-level `git_provider:` key in `config.yaml` declaring which git host the deployment serves — `github` or `bitbucket`. `git_provider: github` reproduces exactly the previous behavior. A config without the key (or with an unknown value) fails startup with an error naming the key and pointing here.

**The upgrade is one added line:**

```yaml
git_provider: github
```

`git_provider: github` requires `GITHUB_WEBHOOK_SECRET` exactly as before (`SLACK_BOT_TOKEN` stays required regardless); nothing else changes. On the first boot after upgrading, every mapping entry revalidates once, because `git_provider` now participates in each entry's lock hash — this is a one-time, idempotent revalidation, not an error.

> ⚠️ **Switching `git_provider` later requires a fresh database.** The provider is *not* recorded per row. If you point an existing database at a different provider, stale rows keyed by the old provider's repository names and PR numbering can collide with the new provider's — silently suppressing posts until the cleanup TTL (`cleanup.message_ttl_days`) purges them. When you change `git_provider`, start from a fresh database (or, not recommended, disable the digest and wait out `message_ttl_days`).

## Ignored-event log fields renamed

The internal git-provider-neutral event refactor renames two fields on the `ignored webhook event` log line: `github_event` becomes `provider` (currently always `github`) and `action` becomes `kind` (a provider-neutral event classification such as `opened`, `merged`, `review_commented`, or `unknown`). No operator action is required to upgrade, but any log dashboards or alerts that filter on `github_event`/`action` should be updated to `provider`/`kind`. See [Operations → Debugging a 200 OK with no Slack change](operations.md#debugging-a-200-ok-with-no-slack-change) for the full field set.

## 0.20.0 — Multi-message fan-out

This release replaces the single `slack_messages` table with normalized `pull_requests` and `messages` tables, so a monorepo PR can fan out to [one Slack message per matched path channel](mappings.md#per-path-routing-monorepos).

> ⚠️ **All in-flight PR tracking is lost on upgrade.** The migration (`00006`) **drops `slack_messages`**. PRs opened *before* the upgrade keep their existing Slack messages, but the server can no longer find them — they receive **no further updates, reactions, or digest entries**. PRs opened *after* the upgrade work normally. This is a deliberate clean cutover: the table holds only transient PR↔message bookkeeping (subject to the cleanup TTL), so it self-heals as new PRs come in. There is no migration of existing rows.

No operator action is required beyond the normal restart. If you want close/merge reactions on a PR that is open across the upgrade, re-announce it (toggle it to draft and back to `ready_for_review`) so the new schema records its messages.

## 0.16.0 — Stuck-PR digest

0.16.0 adds a scheduled [stuck-PR digest](mappings.md#stuck-pr-digest): a per-channel reminder listing open PRs nobody has touched since before today. Two things make this upgrade more than a restart.

> ⚠️ **The digest is on by default.** With no `digest:` section in `config.yaml`, the server starts posting a digest at **9am daily, UTC** (configurable via `digest.timezone` since 0.19.0 — see below), to every mapped channel. This is a deliberate opt-out design. To keep the previous quiet behavior, add the section and disable it:
>
> ```yaml
> digest:
>   enabled: false
> ```

> ⚠️ **The first digest can list already-merged PRs.** Rows created before this release have no closed/open marker, so the digest treats every PR you ever posted about (within the cleanup TTL) as open — often mostly merged PRs. Run the one-time reconcile (step 3) before relying on the digest.

### Upgrade steps

1. **Deploy the new image and restart.** The embedded migration (`00004`) runs on startup: it adds the `closed_at` column and backfills each existing row's `updated_at` from its Slack message timestamp (the PR's registration time) so ages are correct. For a controlled rollout, run `notifycat-migrate up` as a separate step first — see [Operations → migrations](operations.md#stuck-pr-digest).

2. **Decide on the schedule (optional).** The digest is global, configured in `config.yaml`. Defaults to `0 9 * * *`. To change it, or to disable the feature, add a `digest:` section:

   ```yaml
   digest:
     enabled: true          # default true
     schedule: "0 9 * * *"  # standard 5-field cron, evaluated in `timezone`
     timezone: "UTC"        # IANA zone; default UTC (since 0.19.0)
   ```

3. **Reconcile the closed-PR backlog (one-time).** This drops already-merged PRs out of the digest by marking their rows closed from their real GitHub state. It needs `GITHUB_TOKEN` (read access) and the same `DATABASE_URL` the server uses. Preview, then apply:

   ```sh
   docker compose run --rm notifycat /usr/local/bin/notifycat-reconcile -dry-run
   docker compose run --rm notifycat /usr/local/bin/notifycat-reconcile
   ```

   It is idempotent and leaves untouched any PR it cannot read (so a token-scope miss never hides an open PR). See [Operations → Reconciling the closed-PR backlog](operations.md#reconciling-the-closed-pr-backlog-one-time). Without a token, clear stale rows manually instead.

### Behavior changes to be aware of

- **`updated_at` now tracks activity.** Reviews and comments bump it (previously only the PR-open notification did). Side effect: the stale-row cleanup ages an actively-reviewed PR from its last activity rather than its open time — generally more correct.
- **Rollback is partial.** Migrating `00004` down drops `closed_at` but cannot undo the `updated_at` backfill; restore from a database backup if you need the old values. The feature is otherwise safe to disable in place via `digest: { enabled: false }`.

## 0.19.0 — Configurable digest timezone (default UTC)

The stuck-PR digest now runs in an explicit, configurable timezone via the new `digest.timezone` key, defaulting to **UTC**. Previously the schedule was interpreted in Go's `time.Local`, which on the published `FROM scratch` image is always UTC regardless of the `TZ` environment variable (the image ships no timezone database). The docs nonetheless described it as "server-local time" — this release makes the behavior explicit and gives you a knob.

> ⚠️ **The effective default clock is now UTC, stated explicitly.** On the published Docker image the digest already fired on a UTC clock, so for those deployments nothing changes. If you run a binary you built yourself on a host with a non-UTC `time.Local`, the digest previously fired at "9am" in that local zone; with this release it fires at 9am **UTC** unless you set `digest.timezone`. To keep a non-UTC time, set the zone explicitly:
>
> ```yaml
> digest:
>   timezone: "Europe/Kyiv"   # IANA zone name; both the firing time and the cutoff use it
> ```

Notes:

- Named zones (e.g. `Europe/Kyiv`) resolve on the `scratch` image because the binary now embeds the IANA timezone database (`time/tzdata`, ~450KB). Setting `TZ` alone still does nothing — use `digest.timezone`.
- An unrecognized zone fails startup with a descriptive error, the same fail-fast contract as an invalid cron spec.
- `timezone` is **global only**. A per-repo `digest:` override may still set its own `schedule`, but setting `timezone` on a per-repo tier is rejected at parse time — the server runs a single cron clock.
