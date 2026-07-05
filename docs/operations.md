# Operations

Notifycat is designed to be operated as a single process plus a SQLite file. There are no background workers and no
external queue.

## Process Model

Run one `notifycat-server` process behind your normal HTTPS ingress. GitHub posts webhooks to `/webhook/github`, and
Notifycat makes outbound HTTPS calls to Slack.

The server exposes:

| Route | Purpose |
| --- | --- |
| `GET /healthz` | Liveness/readiness check. Returns `200 OK` once the HTTP listener is up — wire it as the target-group health check (ALB, nginx upstream, Cloud Run) or the `livenessProbe`/`readinessProbe` path in Kubernetes. Because startup validation and migrations run before the listener opens, a `200` means the process is healthy in both senses. |
| `POST /webhook/github` | GitHub webhook receiver. |

## Startup and Shutdown

The server fails fast when required configuration is missing. It also applies embedded migrations at startup, so a
simple deployment can start the server directly. For stricter production setups, run `notifycat-migrate up` as a
separate init step before the server starts.

Shutdown listens for `SIGINT` and `SIGTERM` and allows in-flight requests to finish within the configured shutdown
window.

## Persistence

State lives in two places:

- **`config.yaml`** — the declarative source of truth for routing and all non-secret configuration. Edit it in version control and deploy it alongside the binary. The sibling `config.lock` caches successful validation so steady-state boots don't re-contact Slack/GitHub.
- **SQLite** — stores per-PR Slack message timestamps so Notifycat can update the same message across the PR lifecycle.

Back up the SQLite file if losing notification state would be painful. If the database is lost, Notifycat can still receive webhooks, but existing PRs may get new Slack messages because the old Slack timestamp mapping is gone. `config.yaml` and `config.lock` live in your repo, so losing the container's local copy is harmless on the next deploy.

> ⚠️ **Changing `git_provider` requires a fresh database.** The provider is not recorded per row. Pointing an existing database at a different `git_provider` lets stale rows (keyed by the old provider's repository names and PR numbering) collide with the new provider's — silently suppressing posts until the cleanup TTL (`cleanup.message_ttl_days`) purges them. Start from a fresh database when you switch providers. See [Upgrading → `git_provider` is now required](upgrading.md#git_provider-is-now-required).

## Stuck-PR digest

A scheduled job reminds channels about open PRs that have gone unreviewed. It is **on by default** (opt-out) and configured in the global `digest:` section of `config.yaml` — see [Mappings → Stuck-PR digest](mappings.md#stuck-pr-digest) for the schema.

**What counts as stuck.** On each tick (default 9am daily, in the configured `digest.timezone` — UTC unless set) the digest lists every open PR whose last activity predates the start of the current day (also measured in that zone) — so a PR that sat through a previous day with nobody reviewing it shows up that morning. "Activity" is anything Notifycat sees on the PR: the open notification, a review (approve / comment / request-changes), or a PR/line comment — each bumps the row's `updated_at`. Suppressed AI reviews (see [Bot-reviewer suppression](#bot-reviewer-suppression)) intentionally do **not** count, so an AI-only pass still leaves a PR waiting for review. Merged/closed PRs are marked and excluded; a PR converted back to draft is removed entirely.

**One PR, possibly many messages.** A monorepo PR with [per-path routing](mappings.md#per-path-routing-monorepos) fans out to one Slack message per matched channel; close/review events act on every one, and the PR appears in each of those channels' digests until it is merged or closed.

**Delivery.** Two posts per Slack channel: a static parent message that pings the channel's configured `mentions` and carries the count, and a single reply in its thread listing that channel's stuck PRs. Keeping the list in the thread holds the channel feed to one line per channel. Mentions live on the parent because Slack thread replies do not notify the channel. The digest is a separate post — not an update to the per-PR message — so it adds a little noise; set `digest: { enabled: false }` if that is unwanted.

**`updated_at` now tracks activity.** Before this feature `updated_at` only moved when a PR was first announced; it now also moves on every review and comment. A useful side effect: the stale-row cleanup ages an actively-reviewed PR from its last activity rather than its open time. The migration that ships the feature backfills existing open rows' `updated_at` from the Slack message timestamp (the PR's registration time) so the first digest ages them correctly. One caveat: PRs that were already merged/closed before the upgrade are not marked closed in the database (their rows predate the `closed_at` column), so they look open to the digest and can flood the first run. Fix that once with `notifycat-reconcile` (below) before relying on the digest.

### Reconciling the closed-PR backlog (one-time)

`notifycat-reconcile` walks every row the database still believes is open, asks GitHub each PR's real state, and marks the merged/closed ones so they drop out of the digest. It is idempotent — safe to run repeatedly — and a PR it cannot read (e.g. a token-scope miss) is left untouched rather than wrongly hidden. It needs `GITHUB_TOKEN` with read access to the repos and the **same `DATABASE_URL` the server uses**.

Preview first, then apply:

```sh
# docker compose (runs against the service's volumes)
docker compose run --rm notifycat /usr/local/bin/notifycat-reconcile -dry-run
docker compose run --rm notifycat /usr/local/bin/notifycat-reconcile
```

It prints a summary like `reconcile (applied): checked=37 closed=34 still_open=3 errors=0`. A non-zero `errors` count exits non-zero — resolve (usually token scope) and re-run. Run it once after upgrading to the digest feature; going forward the close handler marks `closed_at` itself, so no repeat is needed. Without a `GITHUB_TOKEN` you cannot reconcile this way — clear the stale rows manually instead.

## Logging

Use:

```sh
LOG_FORMAT=json
```

for production log aggregation. Logs avoid raw webhook payloads, signatures, and secrets.

### Debugging a 200 OK with no Slack change

GitHub records a successful delivery whenever Notifycat returns 200 — including when the event is intentionally ignored.
Every silent no-op leaves a structured log line with the message `ignored webhook event` and a `reason` field, so an
operator can answer *"why didn't Slack change for delivery X?"* from logs alone.

Standard field set (all six fields appear on every `ignored webhook event` line):

| Field | Example |
| --- | --- |
| `reason` | `no_handler`, `no_mapping`, `no_stored_message`, `already_sent` |
| `handler` | `open`, `close`, `draft`, `approve`, `commented`, `request_change` (empty for the dispatcher) |
| `provider` | `github` (the git provider the event came from) |
| `kind` | `opened`, `ready_for_review`, `closed`, `merged`, `converted_to_draft`, `approved`, `changes_requested`, `commented`, `review_commented`, `unknown` |
| `repository` | `owner/repo` |
| `pr` | PR number (int) |

The `kind` field is the provider-neutral event classification; the inbound adapter maps each provider's own vocabulary (GitHub's `X-GitHub-Event` header + `action` + review state) onto it. An unmapped delivery — a `synchronize`, a `labeled`, an edited approval — carries `kind=unknown`.

Reasons and their levels:

| `reason` | Level | What it means | Typical fix |
| --- | --- | --- | --- |
| `no_handler` | **Debug** | No registered handler matched this `(provider, kind)` pair. Volumetric — fires for `kind=unknown` deliveries such as `synchronize` and `labeled`. | Expected; set `LOG_LEVEL=debug` to see it. |
| `no_mapping` | **Warn** | Webhook arrived for a repo no `config.yaml` entry covers. | Add the repo to the `mappings:` section of `config.yaml` (or remove the webhook from that repo). |
| `no_stored_message` | **Info** | Handler ran but found no Slack message row for this PR. Common when the PR predates Notifycat. | Re-open the PR (or wait for the next applicable event) so `OpenHandler` can re-announce. |
| `already_sent` | **Info** | `OpenHandler` saw an existing message row — idempotency kicks in. | Expected on `ready_for_review` after a prior `opened`. |

To surface `no_handler` lines during triage:

```sh
LOG_LEVEL=debug
```

`grep` for the `reason` value (or filter on it in your log aggregator) to slice silent deliveries by class.

## Deploying a Release Image

The release workflow runs on `v*` tags and publishes images to:

```text
ghcr.io/mptooling/notifycat:<version>
ghcr.io/mptooling/notifycat:latest
```

For a Git tag such as `v0.1.0`, the version image tag is `0.1.0`.

### Testing a PR build before release

The `docker-pr` workflow publishes `ghcr.io/mptooling/notifycat:pr-<number>` for every open same-repo PR, rebuilt on each push and deleted when the PR closes. Pull that tag on the server to verify a container before merging. To rebuild one manually, run the `docker-pr` workflow from the Actions tab with the PR number. See [Docker → Beta tags](docker.md#beta-tags-per-pull-request).

## Deployment

For end-to-end deploy instructions, see [Install with Docker Compose](compose.md). That page covers the one-command
installer, the interactive setup wizard, HTTPS via Let's Encrypt, and the full preflight checklist.

For local first-time setup against a tunnel (Go source, no Docker), see [Getting started](getting-started.md). For the
manual Docker + host Caddy alternative, see [Docker → Production deploy on a single
VM](docker.md#production-deploy-on-a-single-vm-ec2-example).

The remainder of this page is operations-time reference: what each log line means, what the validate / doctor checks
cover, and how to trace a silent 200-OK delivery.

<!-- Stale anchor preserved for old links to operations.md#deployment-checklist -->
<a id="deployment-checklist"></a>

## Validating a Mapping

`notifycat-config validate` is a non-destructive command that surfaces setup problems before GitHub fires a real PR event. It exits 0 when every check passes (or is skipped) and 1 when any check fails.

```sh
notifycat-config validate                 # check every mapping
notifycat-config validate owner/repo      # check a single mapping
```

Each line in the output is `STATUS  check-name  detail`. `OK`/`FAIL`/`SKIP` are plain ASCII so the output is greppable
in CI logs.

| Check | What it verifies | How to fix a `FAIL` |
| --- | --- | --- |
| `mapping` | An entry exists in `config.yaml` for `owner/repo` (explicit repo tier or wildcard `"*"` tier). | Add a repo tier for the repo, or ensure the org has a `"*"` tier. See [Mappings file](mappings.md). |
| `channel-format` | The entry's channel ID matches `[CGD][A-Z0-9]{2,}`. | Edit `config.yaml` and use the real Slack channel ID, not the display name. |
| `slack-auth` | `auth.test` succeeds and `X-OAuth-Scopes` includes `chat:write` and `reactions:write`. | Rotate `SLACK_BOT_TOKEN`, or reinstall the app after updating the manifest scopes. |
| `slack-channel` | `conversations.info` reports the channel exists, is not archived, and the bot is a member. | `/invite @notifycat` in the channel; unarchive if needed; correct the channel ID. |
| `github-webhook` | When `GITHUB_TOKEN` is set, an active webhook on the repo points at `/webhook/github` and subscribes to `pull_request`, `pull_request_review`, `pull_request_review_comment`, `issue_comment`. Skipped when `GITHUB_TOKEN` is unset. | Create the webhook with `./scripts/github-webhook-create.sh`, or edit the existing webhook to add the missing events. |

### Tokens and Scopes for `validate`

- `SLACK_BOT_TOKEN`: the same bot token the server uses. Scopes `chat:write`, `reactions:write`, and `channels:read` (or
  `groups:read` for private channels) cover every probe.
- `GITHUB_TOKEN` (optional): a PAT with `admin:repo_hook` (or `repo` if the repository is private). Only the validate
  CLI consumes this; the server ignores it.

## Bot-reviewer suppression

When `NOTIFYCAT_IGNORE_AI_REVIEWS=true`, notifycat skips the `reactions.add` call for any review event whose
`sender.type == "Bot"`. The initial PR-open message and all human-reviewer events are unaffected.

### What it covers

The toggle silences every GitHub App and legacy bot account on review and review-comment events. That includes, but is
not limited to:

| Category | Examples |
| --- | --- |
| AI reviewers | Copilot review, Claude review, Codex, Goose, CodeRabbit, Reviewpad. |
| Scripted bots | `dependabot[bot]`, `renovate[bot]`, `github-actions[bot]` auto-approve workflows, `release-please[bot]` self-approvals. |
| Custom CI bots | Anything whose `sender.type` GitHub reports as `Bot`. |

### Bots ≠ AI agents — the explicit trade-off

GitHub's payload does not distinguish AI reviewers from scripted bots. The only signal available is `sender.type ==
"Bot"`. We deliberately do **not** maintain a curated AI-agent allowlist — vendor GitHub Apps get renamed (Copilot's
review bot has already been renamed twice) and such a list rots faster than the operator value it provides.

If you enable `NOTIFYCAT_IGNORE_AI_REVIEWS`, you are opting into a **uniform** rule: every non-human reviewer is
silenced. If a team wants their `github-actions[bot]` auto-approve green checkmark to surface in Slack, they should
leave the flag off.

On `git_provider: bitbucket` the same suppression keys on `actor.type != "user"`, so a bot that authenticates as an ordinary Bitbucket **user account** is indistinguishable from a human and is not suppressed — see [Configuration → Bitbucket behavior notes](configuration.md#bitbucket-behavior-notes) for that blind spot and the self-healing `pullrequest:updated` draft/ready semantics.

This is also intentionally narrow:

- It only affects `reactions.add`. The initial `chat.postMessage` (with mentions / `@channel` fallback) is unchanged.
- It does not look at the PR **author** — a bot-authored PR (e.g. dependabot-created) still posts a new Slack message
  when it opens.
- It does not touch `config.yaml`. No schema change, no migration, no per-mapping override.

### The complement: marking bot reviews instead of hiding them

Suppression and marking are two opposite policies over the same `sender.type == "Bot"` signal, and they are mutually
exclusive:

- **Suppress** (`NOTIFYCAT_IGNORE_AI_REVIEWS=true`) — drop the bot's reaction entirely.
- **Mark** (`NOTIFYCAT_IGNORE_AI_REVIEWS=false`, the default) — react as normal **and** add `SLACK_REACTION_BOT_REVIEW`
  (default `robot_face`) alongside, so a bot's review stays visible but is recognisably non-human.

When suppression is on, the marker never appears — the reaction is skipped before the marker is considered. When
suppression is off, every bot review gets both the normal state reaction (✅ / 💬 / ❗) and the 🤖 marker. To turn the
marker off while still showing bot reviews like human ones, set `SLACK_REACTION_BOT_REVIEW=` (empty). The marker, like
suppression, applies uniformly to every `Bot` sender across `pull_request_review` and `pull_request_review_comment`
events; it does not distinguish AI reviewers from scripted bots.

### Failure-mode guide

> "I expected a green checkmark / speech bubble / red flag on my PR's > Slack message, but nothing happened."

1. Check `notifycat-server` logs at debug level. A line like `level=DEBUG msg="skipped bot reviewer reaction"
   login=copilot[bot] …` confirms the suppression fired.
2. If you see that log, `NOTIFYCAT_IGNORE_AI_REVIEWS` is on and the reviewer's GitHub account is a Bot/App. Either:
   - Disable the flag (`NOTIFYCAT_IGNORE_AI_REVIEWS=false`) — every bot reviewer will react again, AI or not.
   - Or accept the trade-off and leave the flag on.
3. If you do **not** see that log, the suppression did not fire; the missing reaction has a different cause — work
   through the regular "silent 200 OK" debug checklist above.

## Notification message

A new (non-draft, or `ready_for_review`) PR posts a [Block
Kit](https://docs.slack.dev/reference/block-kit) message — a headline section
and a muted context line:

- **Headline `section`** — `:<new-PR emoji>: <mentions>, please review <link|PR #N: title>`.
  Mentions stay in the section because Slack only reliably pings on a mention in
  a section (a context block renders it as gray text but does not notify); with
  no mentions the prefix is omitted, so there is no stranded comma.
- **Context line** — `owner/repo · <author> · opened <localized time>`. The
  time uses Slack's date token, so each viewer sees it in their own timezone
  ("opened Today at 2:04 PM"). When the webhook omits `created_at` the
  "opened …" clause is dropped.

A plain-text fallback is sent alongside the blocks; Slack uses it for the mobile
push preview and screen readers (it does not read interior block text).

On **merge or close** the message updates in place: the title is struck through, the leading emoji swaps to the merged/closed reaction emoji, and a `[Merged]` / `[Closed]` label is prepended. The context line is preserved. If any reviewer started a review session via the "Start review" button before the PR closed, a muted "reviewed by <@user>" context line is appended listing everyone who reviewed it (deduped, in the order they started).

### Review-session lifecycle

When a reviewer clicks "Start review", an active session is opened in the database and a `:eye: <@user> reviewing` marker is appended to the message. A submitted GitHub review (`pull_request_review` with `action: submitted`) automatically finishes all active sessions for that PR — in v1 there is no GitHub-login-to-Slack-user mapping, so any submission closes every open session (Finish is idempotent, so no active session is a silent no-op). When at least one session was active, the message is also taken out of the in-review state: the `:eye: reviewing` markers are cleared and a muted `reviewed by <@user>, …` line replaces them, while the "Start review" button stays so the still-open PR can be picked up again. Because a submission finishes every session at once, all markers clear together and the `reviewed by` line lists everyone who clicked Start review. A submit with no active session is a plain reaction — the message is left untouched. Line comments (`pull_request_review_comment`) and conversation comments (`issue_comment`) do not finish a session — only a true GitHub review submit does. Suppressed bot reviews (when `NOTIFYCAT_IGNORE_AI_REVIEWS=true`) return before the finish gate and therefore do not close a session. On merge or close any remaining active sessions are finished and the same `reviewed by` line is shown alongside the struck-through, `[Merged]`/`[Closed]` message.

## Dependabot / Renovate format

With `NOTIFYCAT_DEPENDABOT_FORMAT=true` (the default), a PR **opened** by
`dependabot[bot]` or `renovate[bot]` posts a compact message instead of the
standard message above — a single line, no context block:

- **Routine bump** — `:package: <mentions>, <bot> bumped <link|PR #N: title>`
- **Security advisory** — `:rotating_light: <mentions>, <bot> security update <link|PR #N: title>`

A few operator-relevant details:

- **Detection is by author login only.** notifycat matches the PR opener's
  login against exactly `dependabot[bot]` and `renovate[bot]` (case-insensitive,
  no prefix matching). Renovate self-hosted instances with custom bot logins are
  not detected.
- **The security split relies on a string in the PR body.** notifycat scans the
  body for a "Vulnerabilities" advisory header. The failure mode is safe: a
  parse miss falls back to the routine `:package:` format, never the reverse. A
  future Dependabot/Renovate template change could therefore regress a security
  PR to the routine format, but will never raise a false `:rotating_light:`.
- **Mentions are unchanged.** Bot PRs use the same `config.yaml` mentions as any other PR — if your entry pings `@channel`, your Dependabot PRs ping `@channel` too.

Set `NOTIFYCAT_DEPENDABOT_FORMAT=false` to render bot-opened PRs with the
standard "please review" format.

## CI Checks

The repository CI runs:

```sh
go vet ./...
golangci-lint run ./...
govulncheck ./...
go test -race ./...
go build ./...
```

The Docker build uses a patched Go toolchain. Keep the Go patch version current when Go security releases land.

For local development, the same checks are available through `just check`. `just` is only a task runner; it is not
included in production builds or Go module dependencies.
