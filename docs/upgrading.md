# Upgrading

Notifycat applies its database migrations automatically on server startup (see [Operations → Startup](operations.md#startup-and-shutdown)), so most upgrades are "pull the new image and restart." This page calls out the releases that need an operator action beyond that.

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
