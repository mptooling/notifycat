# Agent PR feed — design (v1)

> **Status (2026-10-28): approved.** Delivered in three PRs (see "Delivery in 3 PRs").

## Why

Notifycat already receives every PR webhook of the repos it routes. Today it only turns them into Slack messages. This feature lets an AI agent (Claude Code or any other) poll the open PRs of the repos it subscribed to over a secure HTTP API, so it can review its peers' PRs.

The agent polls. Notifycat never calls the agent.

v1 is expected to live about three months. v2 replaces the invite-and-token login with SSO / OAuth. The `/api/v1/subs` contract stays the same in v2, so agents do not change.

## Use case

**Peer review.** An engineer runs an agent in the background. The agent polls notifycat and gets only the open PRs of the subscribed repos that it has not received yet. It reviews each one.

## Decided by Pavlo

- **Generic feature in notifycat upstream.** No code for one host (Fly.io) or one company (EMIL). Operator setup lives in the operator's own deploy repo; the docs show examples.
- **Peer review only.** No self-review: engineers do that in their own pipelines.
- **The admin invites a developer** with a one-time invite. The invite works for **1 hour**. The developer redeems it and gets their own token. The admin never sees the token.
- **Admin endpoints live on a private admin port**, reached through a private tunnel, and also need an admin key.
- **Long-lived tokens are fine for v1.** Deleting a token's hash removes that access at once.
- **Token hashes live only in notifycat's SQLite database, hashed with bcrypt.** Never in git, never in `config.yaml`.
- A developer may keep their token in a `~/.*rc` file.
- **Subscriptions and PRs are separate endpoints.** `GET /subs` lists the subscribed repos. `GET /subs/prs` returns the PRs of those repos. Both need only the token.
- **Changing the list never resends it.** One call adds one or many repos. `DELETE /subs/<id>` removes one subscription.
- A developer can subscribe to any configured repo, or to all of them.
- **HTTPS only** for the public API.
- No per-developer repo limits in v1: every token sees all configured repos.
- A restart of a few seconds (to change config) is fine for v1.
- No admin commands in Slack. No rate limiting or DDoS protection in v1.
- **No author names in the feed (GDPR).** A PR carries only the git host's `author_id`.
- **All PRs are sent, also the developer's own.** No author filter in v1.
- **A PR comes once per token.** `GET /subs/prs` returns only what this token has not received yet. Two calls at the same time never get the same PR: the second gets what the first did not take, usually nothing. New commits do not bring a PR back (maybe later).
- **`?since=<timestamp>`** returns the open PRs changed since that time, also ones already received.

## My calls (check these)

1. **Delivery is tracked per token and per PR.** A normal call returns the subscribed open PRs that this token never received, and records them in the same atomic step. When a PR closes or turns draft, its records go too, so a reopen or a ready-for-review brings it back. A token's first call, and the first call after a new subscription, return all open PRs of those repos. Notifycat still records pushes, so `head_sha` and `updated_at` stay current and `?since=` sees new commits; re-delivery on a new commit can come later without a schema change to the PR table.
2. Two new domains: `internal/access` (admin key, invites, tokens, authentication) and `internal/feed` (subscriptions, open-PR list).
3. Token format `nct_<id>_<secret>`; invite format `nci_<id>_<secret>`. `id` = 16 random bytes (hex), `secret` = 32 random bytes (base64url), both from `crypto/rand`. The database stores `id` in clear and `bcrypt(secret)`. The `id` is needed because a bcrypt hash cannot be searched. A random `id` also means a request with a made-up token never reaches bcrypt.
4. bcrypt cost = Go's `bcrypt.DefaultCost` (10). No in-memory cache of verified tokens, so a deleted token stops working on the next call.
5. Tokens have no end date in v1. Each token row has a `user` label taken from its invite.
6. An invite is single-use. Redeeming it creates the token and marks the invite used in one transaction.
7. The admin key is one random key per deployment. `config.yaml` never holds it. Notifycat reads only its bcrypt hash from the env secret `NOTIFYCAT_ADMIN_KEY_HASH`. A new helper, `notifycat-keygen`, prints a new key (stdout) and its hash (stderr).
8. The admin server listens on `server.admin_addr` from `config.yaml` (empty = no admin server). Startup fails when `admin_addr` is set but `NOTIFYCAT_ADMIN_KEY_HASH` is not, or when `admin_addr` equals `server.addr`.
9. Subscriptions belong to a token. Each subscription is one row with its own id, for one repo or for `"*"`. A token sees and deletes only its own subscriptions; another token's id gives `404`. A "configured repo" is one that `mappings:` routes, exactly or through an `owner/*` tier. `"*"` means every configured repo, including repos added later. A repo that loses its mapping drops out of `GET` at once.
10. Notifycat tracks open, non-draft PRs of **every configured repo**, whether anyone subscribed or not. A new subscription then sees PRs that are already open.
11. Each PR carries `author_id` (GitHub user id / Bitbucket `account_id`), `description`, `source_branch`, `target_branch`, and `created_at`. Title and description are refreshed on every recorded event; an edit without a push shows up with the next push.
12. `agent_api.enabled` defaults to `false`. When off: no `/api/v1/` routes (`404`), no admin server, nothing tracked.
13. Every auth failure gets the same `401`. Only the server log says why.
14. Notifycat still needs its Slack bot token to start. A deployment without Slack is a non-goal.
15. Delivery in three PRs (see the end). The first fixes foreign keys, so deleting a token can cascade to its subscriptions.
16. **PR notifications missed during downtime are accepted in v1.** When notifycat is down (restart, deploy, outage), the git host's notification for that moment is lost and not repaired: a missed "opened" hides the PR until its next push, a missed push hides that head commit, a missed "closed" leaves the PR listed. The agent checks that a PR is still open before it reviews. v2 can add a reconcile against the git host.
17. **Repo names are compared in lower case everywhere**: subscriptions, the configured-repo check, and `feed_pull_requests`. Routing looks repos up case-sensitively today and webhooks keep the host's casing, so the feed's catalog folds both sides itself.
18. Adding uses `POST /api/v1/subs` (add to the collection), not `PATCH /subs` or `POST /subs/add`. It takes one or many repos. A repo that is already subscribed is kept, not an error.
19. **`?since=` only reads.** It returns the subscribed open PRs with `updated_at` at or after `since` (RFC 3339), received before or not, and records nothing. The next normal call returns the same as without it. Delivery is at most once, so `?since=` is how an agent gets back a list it lost (crash, network error).
20. Delivery is per token. Two agents that share one token share one list: each PR goes to only one of them.
21. `GET /subs/prs` stays a `GET`, as Pavlo set it, although a normal call changes state. A proxy that retries a failed `GET` can lose one list; `?since=` gets it back.

## Non-goals (v1)

SSO / OAuth (v2). Self-service sign-up without an invite. Token end dates. More than one admin key, or roles. An admin web UI. Managing `config.yaml` or mappings through the API. Admin commands in Slack. Listing or cancelling pending invites. Rate limiting and DDoS protection. Serving code, diffs, or file lists. Push delivery (webhooks to agents, SSE, WebSocket). Review and comment events. Draft PRs. PRs that were already open before v1 started and get no webhook after. Repairing the PR list after a PR notification missed during downtime. Per-developer repo limits. Hiding a developer's own PRs. Returning a PR again after new commits. An "ack" step after a review: a PR counts as received when it is returned. A "claim" so two tokens never review the same PR. An MCP server. TLS inside notifycat. Any host-specific code (Fly.io, Kubernetes, …). A deployment without Slack.

## Acceptance criteria

- Tokens must not appear in logs.
- Token hashes must not be stored in git.
- Admin routes must not answer on the public port.
- A feed error must not change Slack notifications.
- Notifycat must not return source code.
- The feed must not store or return author names.
- Normal calls must not return the same open PR to one token twice, also when the calls run at the same time.

## How it works

```
admin (laptop)                         notifycat                              developer / agent
──────────────                         ─────────                              ─────────────────
tunnel to the admin port
POST /admin/v1/invites {"user":"alice"}
  ← nci_… + ready curl command ── sent to alice privately ─────────────────→  POST /api/v1/invites/redeem
                                       creates token, stores bcrypt(secret)    ← nct_…  (shown once)
                                                                              NOTIFYCAT_TOKEN=nct_… in ~/.zshrc
                                                                              POST /api/v1/subs {"repositories":["acme/api"]}
GitHub/Bitbucket webhook ────────────→ notification (Slack, as today)
                                       feed recorder → feed_pull_requests
                                                                              GET /api/v1/subs/prs (every few minutes)
                                                                                ← PRs not received yet
                                                                              review each PR
```

## Admin port

A second HTTP server, separate from the public one. It serves the admin routes and nothing else. The public server never registers them.

| Where notifycat runs | `server.admin_addr` | How the admin reaches it |
| --- | --- | --- |
| Docker Compose on a VM | `0.0.0.0:9091` in the container, published on the host only as `127.0.0.1:9091` | `ssh -L 9091:localhost:9091 vm` |
| Kubernetes | `0.0.0.0:9091`, no Ingress for it | `kubectl port-forward` |
| Fly.io | `fly-local-6pn:9091`, no `[[services]]` entry | `fly proxy 9091 -a <app>` |

The admin port speaks plain HTTP. The docs state that it must never be published to the internet; the tunnel provides the encryption.

### Admin routes

All need `Authorization: Bearer <admin key>`, checked with bcrypt against `NOTIFYCAT_ADMIN_KEY_HASH`.

| Route | Result |
| --- | --- |
| `POST /admin/v1/invites` `{"user":"alice"}` | `201 {"invite":"nci_…","user":"alice","expires_at":"…","redeem_command":"curl …"}`. The command uses `server.domain` for the public URL. |
| `GET /admin/v1/tokens` | `{"tokens":[{"id":"…","user":"alice","created_at":"…","last_used_at":"…"}]}`. Never a hash. |
| `DELETE /admin/v1/tokens/{id}` | `204`; the token and its subscriptions are gone. `404` if unknown. |

User labels: `[a-z0-9][a-z0-9._-]{0,63}`.

## Public API

All routes need HTTPS (enforced by the operator's proxy; the docs say so). All responses are JSON with `Cache-Control: no-store`. Errors are `{"error":"<code>"}`.

| Route | Auth | Result |
| --- | --- | --- |
| `POST /api/v1/invites/redeem` | `Authorization: Invite nci_…` | `201 {"user":"alice","token":"nct_…"}`, once. Unknown, used, or expired → `401`. |
| `GET /api/v1/subs` | `Authorization: Bearer nct_…` | `{"subscriptions":[{"id":17,"repository":"acme/api","created_at":"…"}]}` |
| `POST /api/v1/subs` | `Authorization: Bearer nct_…` | Body `{"repositories":["acme/api","acme/web"]}`, or `["*"]` for all. Adds them; a repo already subscribed is kept. `200` with the full list, same shape as `GET`. `400 unknown_repository` lists the repos that are not configured, and nothing is added. Max 200 subscriptions per token; body max 16 KiB. |
| `DELETE /api/v1/subs/{id}` | `Authorization: Bearer nct_…` | `204`. `404` if the id is unknown or belongs to another token. |
| `GET /api/v1/subs/prs` | `Authorization: Bearer nct_…` | See below. |

### `GET /api/v1/subs/prs`

```json
{
  "pull_requests": [
    {
      "repository": "acme/api",
      "number": 42,
      "title": "Add retry to the billing client",
      "description": "Retries 5xx responses up to three times.",
      "url": "https://bitbucket.org/acme/api/pull-requests/42",
      "author_id": "557058:2f1c…",
      "source_branch": "feature/billing-retry",
      "target_branch": "main",
      "head_sha": "9fceb02d0ae5",
      "created_at": "2026-10-27T16:40:12Z",
      "updated_at": "2026-10-28T09:14:03Z"
    }
  ]
}
```

- **Normal call** (no query): open, non-draft PRs of the subscribed repos that are still configured, that this token has not received yet. They are marked received in the same atomic step.
- **`?since=2026-10-28T09:00:00Z`** (RFC 3339): the same repos, every open PR with `updated_at` at or after that time, received or not. Marks nothing. A bad value → `400 invalid_since`.
- Oldest `updated_at` first.
- `created_at` = when the PR was opened on the git host.
- `updated_at` = when notifycat last saw a change to this PR.
- Nothing new, or no subscriptions → an empty list.

## Security rules

- **Bearer check** (public API): parse `nct_<id>_<secret>` → find the row by `id` → `bcrypt.CompareHashAndPassword` → put a `Principal` (token id, user) in the request context. Any failure → `401 {"error":"unauthorized"}` with `WWW-Authenticate: Bearer`, and `WARN agent api auth failed reason=malformed|unknown|mismatch`.
- **Invite check**: same shape with `nci_`, plus not used and not expired.
- **Admin check**: bcrypt against `NOTIFYCAT_ADMIN_KEY_HASH`; failure → `401`, `WARN admin auth failed`.
- **Never log** a token, an invite, the admin key, or an `Authorization` header. A test checks this.
- `last_used_at` is written at most once per minute per token.
- **What a stolen token gets:** titles, descriptions, branch names, author ids, links, and commit SHAs of open PRs in configured repos. No code. The fix: `DELETE /admin/v1/tokens/{id}`.
- **What a stolen invite gets:** one token, within the hour, if nobody redeemed it first.

## Design

### `internal/kernel`

- `PR.HeadSHA string`. GitHub `pull_request.head.sha`; Bitbucket `pullrequest.source.commit.hash`.
- `PR.AuthorID string`. GitHub `pull_request.user.id`; Bitbucket `pullrequest.author.account_id`.
- `PR.SourceBranch string`. GitHub `pull_request.head.ref`; Bitbucket `pullrequest.source.branch.name`.
- `PR.TargetBranch string`. GitHub `pull_request.base.ref`; Bitbucket `pullrequest.destination.branch.name`.
- `PR.Body` and `PR.CreatedAt` already exist. `PR.Author` stays for Slack; the feed never reads it.
- `KindUpdated` (GitHub action `synchronize`) and `KindReopened` (GitHub action `reopened`), with the existing draft gating (a draft maps to `KindUnknown`). Bitbucket mapping does not change: its `pullrequest:updated` already arrives as `ready_for_review` on every push, which carries the new `HeadSHA`.
- No notification handler accepts the new kinds, so the dispatcher logs `no_handler` with `kind=updated` / `kind=reopened` instead of `kind=unknown`. Nothing changes for Slack.

### `internal/access` (template: `internal/review`)

- `domain/`
  - `models.go`: `Principal{TokenID, User}`, `Invite{ID, User, ExpiresAt, UsedAt}`, `Token{ID, User, CreatedAt, LastUsedAt}`, `IssuedInvite{Invite, Secret, RedeemCommand}`, `IssuedToken{Token, Secret}`, `Credential{Prefix, ID, Secret}`. Context helpers `WithPrincipal` / `PrincipalFrom`.
  - `enums.go`: `AuthFailureReason` (`malformed`, `unknown`, `mismatch`, `expired`, `used`).
  - `constants.go`: `TokenPrefix = "nct_"`, `InvitePrefix = "nci_"`, `IDBytes = 16`, `SecretBytes = 32`, `InviteTTL = time.Hour`, `LastUsedResolution = time.Minute`, user-label pattern.
  - `interfaces.go`: ports `InviteStore`, `TokenStore`, `SecretSource`, `Hasher` (bcrypt), `Clock`. Use cases `InviteIssuer`, `InviteRedeemer`, `TokenAdmin` (list, delete), `TokenAuthenticator`, `AdminAuthenticator`.
- `application/`: the use cases.
- `infrastructure/`: `admin_handlers.go`, `redeem_handler.go`, `bearer_middleware.go`, `admin_middleware.go`, `invite_repo.go`, `token_repo.go`, `bcrypt_hasher.go`, `random_secret.go`.
- `module.go` + `module_test.go`. depguard rules `access-domain` / `access-application`.

### `internal/feed`

- `domain/`
  - `models.go`: `Subscription{ID, TokenID, Repository, CreatedAt}` (`Repository` may be `"*"`), `PullRequest{Repository, Number, Title, Description, URL, AuthorID, SourceBranch, TargetBranch, HeadSHA, CreatedAt, UpdatedAt}`.
  - `constants.go`: `AllRepositories = "*"`, `MaxSubscriptionsPerToken = 200`.
  - `interfaces.go`: ports `SubscriptionStore`, `PullRequestStore` (upsert, delete, list changed since), `DeliveryStore` (`Claim`: in one write transaction, return the open PRs of the given repos that the token has not received yet, and record them), `RepositoryCatalog` (`IsConfigured(repo)`, `Configured(repos) []string` — filters to configured repos). Use cases `Subscriptions` (`List`, `Add`, `Remove`), `PullRequests` (`Next`, `Since`), `Recorder.Record(ctx, kernel.Event) error`.
- `application/`
  - `Recorder`: skip repos that are not configured. `opened` / `ready_for_review` / `updated` / `reopened` → upsert. `closed` / `merged` / `converted_to_draft` → delete. Other kinds → nothing.
  - `Subscriptions.Add`: validate every repo against the catalog (all or nothing), lower-case, insert the missing ones.
  - `Subscriptions.Remove`: delete only a subscription of the calling token.
  - `PullRequests.Next`: load the token's subscriptions, keep configured repos (`"*"` = every configured repo), then `DeliveryStore.Claim`.
  - `PullRequests.Since`: the same repos, then `PullRequestStore` list changed since. Records nothing.
- `infrastructure/`: `subs_handler.go` (`GET` / `POST` / `DELETE /subs`) and `prs_handler.go` (`GET /subs/prs`), both taking the `Principal` from `accessdomain.PrincipalFrom(ctx)`, `subscription_repo.go`, `pull_request_repo.go`, `delivery_repo.go`, `routing_catalog.go` (wraps the routing `Provider`).
- `module.go` + `module_test.go`. depguard rules `feed-domain` / `feed-application` (infrastructure may import `access/domain` and `routing`).

### Persistence (`internal/platform/persistence`)

```sql
-- PR 1
access_invites(id TEXT PRIMARY KEY, user TEXT NOT NULL, secret_hash TEXT NOT NULL,
               expires_at DATETIME NOT NULL, used_at DATETIME, created_at)
access_tokens(id TEXT PRIMARY KEY, user TEXT NOT NULL, secret_hash TEXT NOT NULL,
              created_at, last_used_at DATETIME)
-- PR 2
feed_subscriptions(id INTEGER PRIMARY KEY,
                   token_id TEXT NOT NULL REFERENCES access_tokens(id) ON DELETE CASCADE,
                   repository TEXT NOT NULL /* lower case, or '*' */, created_at,
                   UNIQUE(token_id, repository))
feed_pull_requests(repository TEXT NOT NULL, number INTEGER NOT NULL, title TEXT,
                   description TEXT, url TEXT, author_id TEXT,
                   source_branch TEXT, target_branch TEXT, head_sha TEXT NOT NULL DEFAULT '',
                   created_at DATETIME, updated_at DATETIME NOT NULL,
                   PRIMARY KEY(repository, number))
feed_deliveries(token_id TEXT NOT NULL REFERENCES access_tokens(id) ON DELETE CASCADE,
                repository TEXT NOT NULL, number INTEGER NOT NULL,
                delivered_at DATETIME NOT NULL,
                PRIMARY KEY(token_id, repository, number),
                FOREIGN KEY(repository, number) REFERENCES feed_pull_requests(repository, number)
                  ON DELETE CASCADE)
```

- The recorder upserts `feed_pull_requests` with `ON CONFLICT … DO UPDATE`, never `INSERT OR REPLACE`. REPLACE deletes the row first, and the cascade would wipe its deliveries.
- `Claim` must be safe for two calls at the same time: one `BEGIN IMMEDIATE` transaction, or `INSERT … ON CONFLICT DO NOTHING RETURNING` so a call returns only the rows it inserted. The builder picks.

Migration numbers: next free ones (`00010`, `00011` if nothing else lands first; the paused personal-notifications spec also plans `00010`–`00012`).

### Config (`internal/platform/config/config.go`)

```yaml
server:
  admin_addr: ""        # empty = no admin server
agent_api:
  enabled: false
```

- Env secret `NOTIFYCAT_ADMIN_KEY_HASH` (read like `SLACK_SIGNING_SECRET`).
- `fileSchema` gets the two keys (else `KnownFields(true)` rejects them). Update `config.example.yaml`.

### Runtime wiring (`internal/runtime/module.go`)

- Startup checks from call 8.
- Enabled: register `/api/v1/` routes on the public mux. Build a second `http.Server` (same hardened timeouts as `buildServer`) for the admin mux when `admin_addr` is set. Both start and stop through `fx.Lifecycle`; a fatal admin-server error shuts the app down like the public one.
- The inbound webhook handlers get a small fan-out that implements `notificationdomain.EventDispatcher`: run the notification dispatcher as today, then `Recorder.Record` under `context.WithoutCancel`. A record error is logged (`WARN feed pull request not recorded`) and never changes the response.
- Disabled: none of the above.

### `cmd/notifycat-keygen`

Prints a new admin key (`nca_` + 32 random bytes, base64url) to stdout and the env line for its bcrypt hash to stderr, single-quoted: `NOTIFYCAT_ADMIN_KEY_HASH='$2a$10$…'`. The quotes matter: `godotenv` and Docker Compose expand `$` in unquoted values and would silently break the hash. Startup fails if the hash is not a valid bcrypt hash. Reads no config. Added to the `Dockerfile`, so `docker run --rm ghcr.io/mptooling/notifycat notifycat-keygen` works anywhere.

## Agent setup (goes in the docs)

```sh
# ~/.zshrc
export NOTIFYCAT_URL=https://notifycat.example.com
export NOTIFYCAT_TOKEN=nct_…

# once: subscribe (the header comes from stdin, so the token is not on the command line)
curl --fail-with-body -sS -X POST -H @- "$NOTIFYCAT_URL/api/v1/subs" \
  -d '{"repositories":["acme/api","acme/web"]}' <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"
```

Claude Code loop, for example `/loop 5m` with this prompt (or `claude -p` from cron):

> Run `curl --fail-with-body -sS -H @- "$NOTIFYCAT_URL/api/v1/subs/prs" <<<"Authorization: Bearer $NOTIFYCAT_TOKEN"`. For each pull request: check it is still open, read its diff, and post a review. Never print `$NOTIFYCAT_TOKEN`.

The docs show the "read the diff, post the review" step for GitHub (`gh`) and for Bitbucket (REST API). They recommend polling every minute or slower. They show how to get back a lost list: `GET /api/v1/subs/prs?since=<time of the last good poll before the lost one>`, or any earlier time. `since` matches a PR's last change, not when it was delivered, so the time of the lost run itself misses PRs. For a lost first poll, use an old time such as `1970-01-01T00:00:00Z` and skip PRs already reviewed.

## Docs

- New `docs/agent-api.md` under **Use** in `mkdocs.yml`: what it is, enabling it, the admin port per platform (table above), creating the admin key, inviting and removing developers, the agent setup, limits.
- `docs/configuration.md`, `docs/cli.md` (`notifycat-keygen`), `docs/security.md` (token and invite model, what a stolen token gets, admin port), `docs/operations.md` (new logs).
- `README.md` features, `ARCHITECTURE.md` domain map (nine domains), `CLAUDE.md` domain table.

## Logs (new)

```
INFO admin server listening addr=fly-local-6pn:9091
WARN admin auth failed
INFO agent invite created user=alice invite_id=… expires_at=…
INFO agent token created user=alice token_id=…
WARN agent invite redeem failed reason=unknown|mismatch|used|expired
WARN agent api auth failed reason=malformed|unknown|mismatch
INFO agent token deleted user=alice token_id=…
INFO feed subscriptions added user=alice token_id=… count=3
INFO feed subscription removed user=alice token_id=… repository=acme/api
INFO feed pull requests delivered user=alice token_id=… count=3      (only when count > 0)
WARN feed pull request not recorded repository=acme/api pr=42 kind=updated err=...
```

## Delivery in 3 PRs

0. **`fix(persistence): enable foreign keys on every pooled connection`** — today `PRAGMA foreign_keys = ON` runs on one pooled connection only (`internal/platform/persistence/db.go`). Use the DSN `_pragma=foreign_keys(1)`. Regression test first.
1. **`feat(access): admin port, invites and agent tokens`** — `server.admin_addr`, `agent_api.enabled`, `NOTIFYCAT_ADMIN_KEY_HASH`, `access` domain and tables, admin server and routes, redeem route, bearer middleware, `notifycat-keygen` (+ `Dockerfile`), docs for the admin port, keygen, security, config.
2. **`feat(feed): subscriptions and open PR list for agents`** — kernel changes, `feed` domain and tables, fan-out recorder, delivery tracking, `/api/v1/subs` and `/api/v1/subs/prs`, `docs/agent-api.md`.

Before coding: one TDD plan per PR in `docs/superpowers/plans/`. Work in a worktree branch.

## Relation to the personal-notifications spec

`docs/superpowers/specs/2026-10-20-personalized-notifications-design.md` (paused) also has "subscriptions", but for Slack users and DMs. The two features are independent: different subscribers, tables, and domains. Both plan PR 0; whichever lands first does it.

## Verification

- `just check` per PR.
- Unit (testify, TDD, table tests):
  - Credential parsing: prefix, id length, secret length, bad input.
  - Token auth: unknown id never calls bcrypt; mismatch; success puts the `Principal` in the context; every failure → same `401` body and `WWW-Authenticate`.
  - Invite: create → redeem once → second redeem `401`; expired after 1 hour (fake clock); redeem is atomic.
  - Admin auth: wrong key `401`; admin routes `404` on the public mux.
  - Startup: `admin_addr` without hash fails; `admin_addr == addr` fails; disabled → no routes, no admin server.
  - Token delete removes its subscriptions (FK cascade).
  - `last_used_at` written at most once per minute.
  - `POST /subs`: exact repo, repo via `owner/*` tier, `"*"`, unknown repo `400` and nothing added, over 200, lower-casing, an already-subscribed repo is kept.
  - `DELETE /subs/{id}`: own id `204`; another token's id `404`.
  - `GET /subs/prs`: only configured + subscribed repos; `"*"` covers a repo added later; a repo removed from mappings disappears.
  - Delivery: first call returns all open PRs, second call is empty; a new push does not return the PR again; close then reopen returns it again; a new subscription returns that repo's open PRs; two calls at the same time on real SQLite (goroutines) get no PR in common and together get all.
  - `?since=`: returns received PRs too; records nothing (the next normal call is unchanged); bad value `400 invalid_since`.
  - Recorder: upsert and delete per kind; unconfigured repo ignored; `head_sha` updates on `updated` (GitHub) and `ready_for_review` (Bitbucket).
  - Adapters: GitHub `synchronize` → `KindUpdated`, `reopened` → `KindReopened`, draft gating; `HeadSHA`, `AuthorID`, `SourceBranch`, and `TargetBranch` from both providers.
  - Recorder never writes an author name (a test checks the stored row).
  - Fan-out: a record error leaves the webhook response and the notification result unchanged.
  - `notifycat-keygen`: stdout key matches the stderr hash.
- **Secret-leak test:** capture all `slog` output during invite create, redeem, a failed and a successful bearer auth, and admin auth; assert no token, invite, or admin key appears.
- FK test (PR 0) on several pooled connections.
- Integration in `internal/runtime`: admin server on a second listener → create invite → redeem on the public server → `POST /subs` → signed GitHub `opened` → `GET /subs/prs` lists it → `GET` again is empty → `synchronize` → `GET` still empty, `?since=` lists it with the new `head_sha` → `closed` removes it → `DELETE` token → `401`.
- Manual: `just serve` with `admin_addr: 127.0.0.1:9091`, run the full flow with curl, then one Claude Code loop against a test PR.
