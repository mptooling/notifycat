# Per-PR multi-message fan-out — `pull_requests` + `messages` redesign

**Status:** approved design, pre-implementation
**Date:** 2026-06-29
**Builds on:** #130 (per-path single-winner resolution), #131 (per-path runtime). This redesign **supersedes** the single-winner routing those introduced.

## Motivation

The store keeps exactly one Slack message per PR: `slack_messages` with composite primary key `(pr_number, gh_repository)` and no channel column (the channel is re-resolved on every read). That model cannot express the reality per-path routing created: a PR whose changed files touch several path-owned directories concerns several teams in several channels.

Per-path routing currently resolves a **single winner** — one channel, with the other directories' mentions unioned into it. The goal now is **multi-channel fan-out**: a PR posts a separate, independently-tracked message to *each* matched channel, and every later event (close, draft, review) acts on all of them.

That requires a one-to-many model — one PR, many messages — which the single-table schema cannot hold. This is the foundation previously deferred as the multi-channel fan-out follow-up (#124).

## Goals

- Model one PR → many messages, each message recording the channel it lives in.
- Fan a PR out to every matched path channel at open time; act on all of them thereafter.
- Make later events read the stored messages instead of re-resolving routing (eliminating the re-resolution drift flagged in #131).
- Adapt the digest, cleanup, and reconcile flows to the PR-level lifecycle.
- Introduce a `messenger` abstraction so Slack is one concrete implementation, not the domain language.

## Non-goals (future work)

- **AI-decided fan-out cap.** No cap for now; a future enhancement may collapse an over-broad fan-out using an AI decider. The M5 "fall back to base above N directories" valve from single-winner routing is removed, not replaced.
- **Stored per-message mentions for precise path-team digest pings.** Not stored for now (see Digest); can be added later without another model change beyond one column.
- **GitHub Enterprise URLs in the digest.** Pre-existing limitation, unchanged.
- **Backfilling/preserving in-flight PRs across the upgrade.** Clean cutover only (see Migration).

## Domain language: the messenger abstraction

Slack becomes one concrete implementation behind a `messenger` abstraction. Concretely:

- The consumer interface `pullrequest.SlackClient` is renamed **`Messenger`** (still satisfied by `slack.Client` — Slack remains the only implementation today).
- The Slack-specific `ts` (thread timestamp) becomes the messenger-agnostic **`message_id`** — the messenger's id for a posted message — in the schema, models, and interface parameters.
- A **channel** is a room in the messenger (the term is generic enough to keep).

This is vocabulary only; no behavior changes from the rename.

## Schema

Two tables replace `slack_messages`.

```
┌─────────────────────────────────────────────┐
│ pull_requests                                │
│ one row per PR — lifecycle + stats           │
├─────────────────────────────────────────────┤
│ PK  id              INTEGER  autoincrement    │
│     gh_repository   TEXT      NOT NULL        │
│     pr_number       INTEGER   NOT NULL        │
│     created_at      TIMESTAMP NOT NULL        │  first seen (stats)
│     updated_at      TIMESTAMP NOT NULL        │  activity clock (digest idle, cleanup TTL)
│     closed_at       TIMESTAMP NULL            │  set on merge/close; NULL = open
├─────────────────────────────────────────────┤
│ UNIQUE (gh_repository, pr_number)             │  natural key
└─────────────────────────────────────────────┘
                     │ 1
                     │
                     │ N        ON DELETE CASCADE
                     ▼
┌─────────────────────────────────────────────┐
│ messages                                     │
│ one row per posted message (PR → many)       │
├─────────────────────────────────────────────┤
│ PK  id               INTEGER  autoincrement   │
│ FK  pull_request_id  INTEGER  NOT NULL  ──────┼──→ pull_requests(id)
│     channel          TEXT     NOT NULL        │  room in the messenger
│     message_id       TEXT     NOT NULL        │  messenger's id for the post (was Slack `ts`)
├─────────────────────────────────────────────┤
│ UNIQUE (pull_request_id, channel)             │  ≤ one message per channel per PR
└─────────────────────────────────────────────┘
```

- **`pull_requests`** holds the PR-level facts. `created_at` is for later statistics; `updated_at` is the activity clock driving the digest's idle detection and cleanup TTL; `closed_at` marks merged/closed so the digest skips it.
- **`messages`** holds one row per posted message. `(pull_request_id, channel)` is unique — at most one message per channel per PR — which makes the open fan-out idempotent on replay.
- Cleanup deletes a stale `pull_requests` row and the cascade removes its messages.

Cascade delete needs SQLite `PRAGMA foreign_keys = ON` on the connection. Implementation confirms it is set (in `internal/store/db.go`); if not, the repository deletes child messages explicitly before the parent.

## Resolution & fan-out (open)

Routing splits into two concerns that today are fused inside `store.RepoMapping`:

- **Per-repo behavior** — `reactions`, `ignore_ai_reviews`, `dependabot_format`. Resolved from the repo tier via `mappings.Get(repo)`. No changed files needed.
- **Per-channel targets** — a list of `Target{ Channel, Mentions }`. This is what fan-out produces.

**Target resolution** (replaces single-winner `GetForFiles`):

1. changed files → matched path rules (most-specific rule per file, exactly as #130's matching).
2. group matched rules by their resolved channel (the rule's own `channel`, else the base channel).
3. each distinct channel → one `Target`; its `Mentions` = the union of that group's rules' mentions (a rule with no `mentions` inherits base; `mentions: []` contributes nobody).
4. **no match** (or repo has no `paths:`, or no token) → a single `Target{ base channel, base mentions }` — unchanged routing.
5. **no cap** — the single-winner M1 tie-break and the M5 fall-back valve are deleted.

The file-fetching `Router` (from #131) now serves **only** the open handler; it still fetches changed files when a token is configured and the repo has `paths:`, and still falls back softly to the single base target on a fetch error.

**Open handler flow** (retry-safe fan-out):

- resolve behavior + targets.
- for each target: if a `messages` row already exists for `(PR, channel)`, skip it; otherwise post the message and insert the row. The `pull_requests` row is created on the first message.
- Per-channel idempotency means a GitHub redelivery — or a partial failure where some posts succeeded and others did not — re-runs cleanly and only posts the missing channels. This is stronger than today's all-or-nothing `already_sent` check.

## Lifecycle handlers (read the stored messages)

Close, draft, and review stop re-resolving routing and instead iterate the PR's `messages` rows.

- **Close** → load messages (none → `no_stored_message`, skip); behavior from `mappings.Get(repo)`; for each `(channel, message_id)`: `UpdateMessage`, then `AddReaction` if reactions are enabled; finally `MarkClosed(repo, number)`.
- **Draft** → load messages; `DeleteMessage` each; delete the `pull_requests` row (messages cascade).
- **Review** (approve / commented / request_change) → load messages; behavior from `mappings.Get(repo)` (AI-ignore gate, emoji, bot marker); `AddReaction` on each message; `Touch(repo, number)` to bump the activity clock.

**Partial-failure safety.** Returning an error triggers GitHub redelivery, and the messenger operations are idempotent on replay: `already_reacted` is already a no-op, and `UpdateMessage` / `DeleteMessage` treat "already done / message not found" as non-errors so a retry over the full message set is safe.

## Digest, cleanup, reconcile

- **Digest.** `FindStuck` returns stale `pull_requests` (old `updated_at`, `closed_at` NULL) together with their messages. Stuck PRs are grouped by `message.channel`; one reminder is posted per channel. A stale PR with no message rows is ignored. **Mentions:** without a stored per-message mentions column, the digest pings `mappings.Get(repo).Mentions` only when the grouped channel is that repo's base channel; reminders in path channels are posted without an @-ping (the room still sees the list). This is the one accepted imperfection of not storing mentions, and is recoverable later by adding the column.
- **Cleanup.** `DeleteStaleBefore(cutoff)` deletes stale `pull_requests`; messages cascade. Same TTL semantics, now at the PR level.
- **Reconcile.** `ListOpen()` returns open `pull_requests`; each is checked against its real GitHub state and `MarkClosed` when merged/closed. Same logic, PR level.

## Store repository & consumer interfaces

- `store.SlackMessages` becomes **`store.PullRequests`**, with models `PullRequest` and `Message`.
- New repository surface (names indicative):
  - `EnsureMessage(ctx, repo, number, channel, messageID)` — create the PR row if absent and insert the message; relies on the `(pull_request_id, channel)` unique key for idempotency.
  - `Messages(ctx, repo, number) ([]Message, error)` — the PR's messages, `ErrNotFound` if the PR is unknown.
  - `Touch(ctx, repo, number)` / `MarkClosed(ctx, repo, number)` / `Delete(ctx, repo, number)`.
  - `FindStuck(ctx, cutoff) ([]StuckPR, error)` where `StuckPR` carries the PR plus its messages.
  - `ListOpen(ctx) ([]PullRequest, error)`.
  - `DeleteStaleBefore(ctx, cutoff) (int64, error)`.
- The consumer interfaces in `pullrequest`, `digest`, `cleanup`, and `reconcile` are updated to match, keeping the consumer-package-interface convention.

## Migration & upgrade

- One goose migration creates `pull_requests` and `messages` and **drops `slack_messages`**. Clean cutover — no data is carried over.
- **Upgrade guide must warn loudly**: all in-flight PR tracking is lost on upgrade. PRs opened before the upgrade keep their existing Slack messages but receive no further updates, reactions, or digest entries (their tracking rows are gone). New PRs opened after the upgrade are unaffected. This is acceptable because the data is transient operator state with a TTL and self-heals for every subsequent PR.

## Scope & phasing

In scope: schema + migration, fan-out target resolution and the open handler, lifecycle handlers reading stored messages, digest/cleanup/reconcile adaptation, the messenger rename, and docs.

The change is large and is expected to land as a few sequential PRs under this one spec:

1. Store models, repository, and the migration (with `slack_messages` dropped).
2. Target resolution + the open fan-out (and the `Messenger` rename).
3. Lifecycle handlers (close/draft/review) reading stored messages.
4. Digest, cleanup, reconcile adaptation.
5. Docs: mappings/operations updates and the upgrade-guide warning.

## Accepted trade-offs

- **Frozen at open.** A PR's message set is decided at open; if its files later change to touch a new directory, that channel is not added, and a channel that stops being touched keeps its message. Chosen for determinism and to eliminate re-resolution drift.
- **No path-team digest pings** until per-message mentions are stored (above).
- **Clean-cutover data loss** on upgrade (above).
