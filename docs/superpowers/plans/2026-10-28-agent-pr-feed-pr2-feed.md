# Subscriptions and open PR list for agents — Implementation Plan (agent PR feed, PR 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An agent with a bearer token subscribes to configured repos (`/api/v1/subs`) and polls `GET /api/v1/subs/prs`, which returns each open, non-draft PR of those repos at most once per token, safely under concurrent calls; `?since=` re-reads without recording.

**Architecture:** The kernel gains four PR fields and two kinds (`updated`, `reopened`) that both webhook adapters fill. A new `internal/feed` domain (template: `internal/access`) owns subscriptions, the PR catalog, delivery tracking, the recorder, and the HTTP adapters. `internal/runtime` wraps the notification dispatcher in a recording fan-out and plugs the feed routes into PR 1's `agent_api_routes` group — both only when `agent_api.enabled`. Migration `00011` adds `feed_subscriptions`, `feed_pull_requests`, `feed_deliveries`; `persistence.Open` gains a default `busy_timeout`.

**Tech Stack:** Go 1.27.3, uber/fx v1.24.0, GORM + `github.com/glebarez/sqlite` (modernc SQLite, supports `RETURNING`), goose v3, testify.

**Spec:** `docs/superpowers/specs/2026-10-27-agent-pr-feed-design.md` — "Delivery in 3 PRs" item 2, plus "Decided by Pavlo", "My calls" 1, 9, 10, 11, 17–21, "Acceptance criteria", "Public API" (all `/subs` rows), "`internal/kernel`", "`internal/feed`", "Persistence" (the three `-- PR 2` tables), "Runtime wiring" (the fan-out), "Agent setup", "Docs", "Logs (new)" (the feed lines), "Verification" (the feed lines and the integration flow).

## Global Constraints

- DDD + hexagonal + uber/fx per CLAUDE.md: every use case and every infrastructure service has an interface in `internal/feed/domain/interfaces.go`; enums in `enums.go`, constants in `constants.go`; >3 args to an exported func/constructor → a DTO (domain layer; `persistence` keeps its own DTOs in `models.go`); doc comments on interfaces only; one `fx.Module`.
- `feed/domain` imports only the kernel and the standard library. `feed/application` imports only `feed/domain` and the kernel. `feed/infrastructure` may import `access/domain`, `routing/domain`, `platform/persistence`, `platform/httpx`.
- Readable names (no `m`, `d`, `rc`, `tc`); table tests use `testCases`/`testCase`.
- testify; `(t, want, got)`; arrange / act / assert; `require` for setup, `assert` for final checks; only `assert` inside httptest handlers.
- Minimal comments: none that restate code.
- Conventional Commits; never mention AI/Claude in commit messages; no Co-Authored-By trailer; never the literal string `BREAKING CHANGE`. PR title / squash commit: `feat(feed): subscriptions and open PR list for agents`.
- Foreign keys are ON on every connection. A migration never rebuilds a parent table. Upserts use `ON CONFLICT … DO UPDATE`, never `INSERT OR REPLACE` (REPLACE deletes the row, and the cascade wipes its deliveries).
- Repo names are lower case in every feed table and every feed response (spec call 17).
- Tokens and `Authorization` headers never reach logs. Feed logs name `user` and `token_id` only.
- Every `/api/v1/subs*` response is JSON with `Cache-Control: no-store` (PR 1's `BearerMiddleware` sets it); errors are `{"error":"<code>"}`. Body cap 16 KiB.
- Log messages and fields exactly as the spec's "Logs (new)" (the feed lines).
- **Task gate** (the "run the task gate" step in each task):

  ```bash
  go vet ./... && go test -race ./... && go build ./...
  docker run --rm -v "$PWD":/app -w /app -v "$(go env GOMODCACHE)":/go/pkg/mod golangci/golangci-lint:v2.14.0 golangci-lint run ./...
  ```

  Expected: every command exits 0. (`just` and a working local golangci-lint are missing here.)

## Decisions the spec leaves open (settled here)

1. **Fan-out order.** The recording dispatcher runs the notification dispatcher first, then `Recorder.Record` under `context.WithoutCancel`, **also when the notification dispatch failed**, and returns the dispatch error unchanged. A record error logs `WARN feed pull request not recorded` and is never returned. With `agent_api.enabled: false` the webhook routes get the plain dispatcher.
2. **Claim.** One GORM transaction whose **first** statement is `INSERT INTO feed_deliveries (token_id, repository, number, delivered_at) SELECT ?, repository, number, ? FROM feed_pull_requests WHERE repository IN ? ON CONFLICT DO NOTHING RETURNING repository, number`; the second statement reads those PR rows in the same transaction. The primary key makes delivery at-most-once; writing first means the transaction never upgrades a read lock (no deadlock `SQLITE_BUSY`).
3. **Busy timeout.** `persistence.Open` adds `_pragma=busy_timeout(5000)` to the DSN unless the DSN already sets `busy_timeout`, so a concurrent writer waits instead of failing with `database is locked`.
4. Times are stored in UTC (`.UTC()` on every clock reading and on `since`). Responses use RFC 3339, UTC, second precision. `created_at` is `null` when the payload had none.
5. `POST /subs` body: `{"repositories":[...]}`, `DisallowUnknownFields`. Entries are lower-cased and de-duplicated (first-seen order). `"*"` is allowed. An empty or missing list → `400 invalid_body`. Unknown repos → `400 {"error":"unknown_repository","repositories":[<lower case, sorted>]}`. Existing + new (after de-dup, repos already subscribed not counted) > 200 → `400 too_many_subscriptions`. In both 400 cases nothing is added. Two concurrent adds of one token may pass 200 by one batch — accepted.
6. `DELETE /subs/{id}`: an id that is not a positive integer → `404 not_found`, without calling the use case.
7. `GET /subs/prs?since=`: present but empty or not RFC 3339 → `400 invalid_since`. Other query parameters are ignored.
8. A request without a `Principal` in the context (cannot happen behind the bearer middleware) → `401 unauthorized`. A store failure → `500 internal_error`, logged at ERROR as `agent api request failed` (same message as PR 1).
9. `feed subscriptions added` is logged only when `count > 0`, `count` = rows newly inserted. `feed pull requests delivered` only when `count > 0`.
10. Order: subscriptions by `id`; PRs by `updated_at`, then `repository`, then `number`.
11. The catalog is built once from `routing` entries at construction (config changes need a restart anyway). A repo that loses its mapping keeps its `feed_pull_requests` rows, but they are never served and its webhooks are no longer recorded.
12. `feed/domain` cannot import `access/domain`, so it has its own `Subscriber{TokenID, User}`, `ErrorCode` enum, and `Clock` port. The HTTP adapter maps `accessdomain.Principal` to `Subscriber`.
13. The recorder stores `event.PR.URL` as-is and `event.PR.Body` as `description`. It never reads `event.PR.Author` or `event.Sender`.

## Review Focus

- A webhook or a subscription with mixed-case names (`Acme/API`, mapping key `Acme`) must match lower-case subscriptions and `owner/*` tiers, and the feed must show `acme/api` — Tasks 3 and 4 pin it.
- Two agents sharing one token poll at the same moment on real SQLite: neither may get `500` from `database is locked`, and no PR may reach both — Task 2 pins it (goroutines through `NewTestDB`, which uses the production `Open`).
- A push to a draft PR (GitHub `synchronize` on a draft, Bitbucket `pullrequest:updated` on a draft) must not list the PR — Task 1 pins the kinds, Task 3 pins the delete.
- `?since=2026-10-28T11:00:00+02:00` must mean the same instant as `09:00:00Z`, and a PR whose `updated_at` equals `since` is included — Tasks 2 and 5 pin it.
- A webhook delivered while Slack fails still records the PR and still answers `500` as today; a record failure with Slack OK still answers `200` — Task 6 pins it.

---

### Task 1: Kernel fields and kinds; GitHub and Bitbucket adapters

**Files:**
- Modify: `internal/kernel/pr.go`, `internal/kernel/enums.go`
- Modify: `internal/notification/infrastructure/inbound.go`, `internal/notification/infrastructure/inbound_bitbucket.go`
- Test: `internal/notification/infrastructure/inbound_kind_test.go`, `inbound_payload_test.go`, `inbound_bitbucket_payload_test.go`, `inbound_bitbucket_kind_test.go`; `internal/kernel/enums_test.go` (create if absent); `internal/runtime/integration_test.go`
- Add to git: this plan

**Interfaces:**
- Produces: `kernel.PR.HeadSHA`, `kernel.PR.AuthorID`, `kernel.PR.SourceBranch`, `kernel.PR.TargetBranch` (all `string`); `kernel.KindUpdated` (`"updated"`), `kernel.KindReopened` (`"reopened"`), appended after `KindReviewCommented` so existing values do not move.

- [ ] **Step 1: Write the failing tests**

  - `inbound_kind_test.go`: the existing case "pull_request synchronize is unmapped" becomes `synchronize` → `KindUpdated`. New cases: `synchronize` with `"draft":true` → `KindUnknown`; `reopened` → `KindReopened`; `reopened` with `"draft":true` → `KindUnknown`.
  - `inbound_payload_test.go` `TestParsePayload_FeedFields`: a body with `pull_request.head.sha "9fceb02d0ae598e95dc970b74767f19372d61af8"`, `head.ref "feature/billing-retry"`, `base.ref "main"`, `user.id 583231` → the event's `PR.HeadSHA`, `SourceBranch`, `TargetBranch` equal those, `AuthorID == "583231"`. A body without `user.id` → `AuthorID == ""`.
  - `inbound_bitbucket_payload_test.go` `TestBitbucketPayload_FeedFields`: `pullrequest.source.commit.hash "9fceb02d0ae5"`, `source.branch.name "feature/billing-retry"`, `destination.branch.name "main"`, `author.account_id "557058:2f1c"` → the same four event fields.
  - `inbound_bitbucket_kind_test.go`: `pullrequest:updated` on an OPEN non-draft PR still maps to `KindReadyForReview` and carries the new `HeadSHA` (no mapping change).
  - `internal/kernel/enums_test.go` `TestEventKindString`: `KindUpdated.String() == "updated"`, `KindReopened.String() == "reopened"`.
  - `internal/runtime/integration_test.go` `TestIntegration_SynchronizeMakesNoSlackCall`: a signed `synchronize` webhook for a mapped repo → `200`, and the Slack fake recorded no call.

- [ ] **Step 2: Run them and see them fail**

  Run: `go test ./internal/kernel/... ./internal/notification/infrastructure/... ./internal/runtime/... -run 'Kind|FeedFields|Synchronize'`
  Expected: FAIL (undefined fields/kinds; synchronize maps to `KindUnknown`).

- [ ] **Step 3: Implement**

  - Kernel: the four fields on `PR`; the two kinds and their `String` cases.
  - GitHub (`inbound.go`): `rawPayload.PullRequest` gains `Head{SHA string "sha"; Ref string "ref"}`, `Base{Ref string "ref"}`, `User.ID int64 "id"`; `PullRequest` gains the four fields (`AuthorID` = `strconv.FormatInt(id, 10)`, empty when `id == 0`); constants `ghActionSynchronize = "synchronize"`, `ghActionReopened = "reopened"`; `lifecycleKind` maps both with the same draft gating as `opened`; `toEvent` copies the fields.
  - Bitbucket (`inbound_bitbucket.go`): `rawBitbucketPayload.PullRequest` gains `Source{Commit{Hash}; Branch{Name}}`, `Destination{Branch{Name}}`, `Author.AccountID "account_id"`; `bitbucketPullRequest` and `toBitbucketEvent` carry them. `mapBitbucketKind` does not change.

- [ ] **Step 4: Run the tests and see them pass**, then **run the task gate**.

- [ ] **Step 5: Commit**

  ```bash
  git add docs/superpowers/plans/2026-10-28-agent-pr-feed-pr2-feed.md internal/kernel internal/notification/infrastructure internal/runtime/integration_test.go
  git commit -m "feat(kernel): head sha, author id, branches and updated/reopened kinds"
  ```

---

### Task 2: Busy timeout, migration 00011 and the feed stores

**Files:**
- Modify: `internal/platform/persistence/db.go`, `internal/platform/persistence/models.go`
- Create: `internal/platform/persistence/migrations/00011_feed.sql`, `internal/platform/persistence/feed.go`
- Test: `internal/platform/persistence/db_test.go`, `internal/platform/persistence/feed_test.go` (reuse `seedToken` from `access_test.go`)

**Interfaces:**
- Consumes: `access_tokens(id)` (PR 1).
- Produces (package `persistence`):
  - Models: `FeedSubscription{ID int64; TokenID string; Repository string; CreatedAt time.Time}` → `feed_subscriptions`; `FeedPullRequest{Repository string; Number int; Title, Description, URL, AuthorID, SourceBranch, TargetBranch, HeadSHA string; CreatedAt *time.Time; UpdatedAt time.Time}` → `feed_pull_requests`; `FeedDelivery{TokenID string; Repository string; Number int; DeliveredAt time.Time}` → `feed_deliveries`. Time fields carry `autoCreateTime:false` / `autoUpdateTime:false` so callers' clocks win.
  - DTOs: `FeedSubscriptionBatch{TokenID string; Repositories []string; CreatedAt time.Time}`, `FeedClaim{TokenID string; Repositories []string; DeliveredAt time.Time}`.
  - `NewFeedSubscriptions(db) *FeedSubscriptions`: `List(ctx, tokenID string) ([]FeedSubscription, error)`; `Add(ctx, batch FeedSubscriptionBatch) (int, error)` (rows inserted; `ON CONFLICT(token_id, repository) DO NOTHING`; empty batch → 0 with no SQL); `Delete(ctx, tokenID string, id int64) (FeedSubscription, error)` (`DELETE … WHERE id = ? AND token_id = ? RETURNING …`; no row → `ErrNotFound`).
  - `NewFeedPullRequests(db) *FeedPullRequests`: `Upsert(ctx, pullRequest FeedPullRequest) error` (`ON CONFLICT(repository, number) DO UPDATE` every non-key column); `Delete(ctx, repository string, number int) error` (missing row is not an error); `Repositories(ctx) ([]string, error)` (distinct, sorted); `ChangedSince(ctx, repositories []string, since time.Time) ([]FeedPullRequest, error)` (`updated_at >= since`; empty `repositories` → empty, no SQL).
  - `NewFeedDeliveries(db) *FeedDeliveries`: `Claim(ctx, claim FeedClaim) ([]FeedPullRequest, error)` per Decision 2; empty `Repositories` → empty, no SQL.

- [ ] **Step 1: Write the failing tests**

  - `db_test.go` `TestOpen_SetsBusyTimeoutOnEveryPooledConnection`: `NewTestDB`, hold 3 connections → `busy_timeout` is `[5000, 5000, 5000]`. The existing `TestOpen_KeepsOperatorQueryParameters` (operator `7000` wins) stays green.
  - `feed_test.go` (seed tokens `t1`, `t2` first):
    - `TestFeedSubscriptions_AddKeepsExisting`: add `[acme/api, acme/web]` → `2`; add `[acme/web, acme/ops]` → `1`; `List(t1)` → `acme/api, acme/web, acme/ops` in id order.
    - `TestFeedSubscriptions_DeleteOnlyOwn`: `Delete(t2, idOfT1Row)` → `ErrNotFound` and the row stays; `Delete(t1, id)` → returns the row with `Repository == "acme/api"`; `List(t1)` empty.
    - `TestFeedTables_TokenDeleteCascades`: a subscription and a delivery for `t1`; delete `access_tokens` row `t1` → 0 rows in `feed_subscriptions` and `feed_deliveries` for `t1`.
    - `TestFeedPullRequests_UpsertUpdatesInPlaceAndKeepsDeliveries`: upsert PR 42, claim it for `t1`, upsert PR 42 with new `HeadSHA` and `Title` → the row shows the new values and `feed_deliveries` still has 1 row (proves no REPLACE).
    - `TestFeedPullRequests_DeleteCascadesDeliveries`: delete PR 42 after a claim → 0 deliveries; deleting a missing PR → no error.
    - `TestFeedPullRequests_ChangedSince`: PRs at `09:00`, `10:00`, `11:00` UTC in `acme/api` plus one in `acme/web`; `ChangedSince([acme/api], 10:00Z)` → `10:00`, `11:00` in that order (the boundary is included; `acme/web` excluded). `since` given as `12:00+02:00` returns the same as `10:00Z`.
    - `TestFeedPullRequests_Repositories`: rows in `acme/web`, `acme/api`, `acme/api` → `[acme/api, acme/web]`.
    - `TestFeedDeliveries_ClaimReturnsOnlyUndelivered`: 2 PRs in `acme/api`, 1 in `acme/web`; `Claim(t1, [acme/api])` → the 2 PRs ordered by `updated_at`; again → empty; `Claim(t2, [acme/api])` → the 2 PRs; `Claim(t1, [acme/api, acme/web])` → only the `acme/web` PR.
    - `TestFeedDeliveries_ClaimConcurrentCallersShareNothing`: 20 PRs, 8 goroutines claim for `t1` behind a closed-channel start barrier; collect results, assert after `Wait`: every error is nil, no `(repository, number)` appears twice, the union has 20.
    - `TestFeedDeliveries_ClaimWithoutRepositories`: `Claim(t1, nil)` → empty, nil error.

- [ ] **Step 2: Run them and see them fail**

  Run: `go test -race ./internal/platform/persistence/... -run 'BusyTimeout|Feed'`
  Expected: FAIL (busy_timeout is 0; feed types undefined).

- [ ] **Step 3: Implement**

  - `db.go`: `busyTimeoutPragma = "_pragma=busy_timeout(5000)"`; the DSN builder appends it unless the DSN contains `busy_timeout` (foreign keys stay always-on).
  - `00011_feed.sql`: the three tables exactly as the spec's `-- PR 2` block (`DATETIME` columns; `created_at` of `feed_subscriptions` `NOT NULL`), plus `CREATE INDEX feed_deliveries_pull_request ON feed_deliveries(repository, number);` for the cascade. Down drops `feed_deliveries`, `feed_pull_requests`, `feed_subscriptions`.
  - `models.go`, `feed.go` per the Interfaces block. Use `clause.OnConflict` for `Add` and `Upsert`, `Raw(...).Scan` for the two `RETURNING` statements.

- [ ] **Step 4: Run the tests and see them pass**, then **run the task gate**.

- [ ] **Step 5: Commit**

  ```bash
  git add internal/platform/persistence
  git commit -m "feat(persistence): feed tables, at-most-once claim and busy timeout"
  ```

---

### Task 3: Feed domain contracts and use cases

**Files:**
- Create: `internal/feed/domain/{doc.go,models.go,enums.go,constants.go,errors.go,interfaces.go}`
- Create: `internal/feed/application/{doc.go,recorder.go,subscriptions.go,pull_requests.go}` and their `_test.go` files plus `fakes_test.go`
- Modify: `internal/routing/domain/interfaces.go` (`EntryLister`), `internal/routing/application/provider.go` (compile-time assertion), `.golangci.yml` (depguard `feed-domain`, `feed-application`, same shape as `access-*`)

**Interfaces:**
- Produces (`feed/domain`):
  - `Subscriber{TokenID, User string}`; `Subscription{ID int64; TokenID, Repository string; CreatedAt time.Time}`; `PullRequest{Repository string; Number int; Title, Description, URL, AuthorID, SourceBranch, TargetBranch, HeadSHA string; CreatedAt, UpdatedAt time.Time}`; `NewSubscriptions{TokenID string; Repositories []string; CreatedAt time.Time}`; `Claim{TokenID string; Repositories []string; DeliveredAt time.Time}`; `SubscriptionsParams{Subscriptions SubscriptionStore; Catalog RepositoryCatalog; Clock Clock; Logger *slog.Logger}`; `PullRequestsParams{Subscriptions SubscriptionStore; PullRequests PullRequestStore; Deliveries DeliveryStore; Catalog RepositoryCatalog; Clock Clock; Logger *slog.Logger}`.
  - Constants: `AllRepositories = "*"`, `MaxSubscriptionsPerToken = 200`, `MaxRequestBodyBytes = 16 << 10`, `ListSubscriptionsPattern = "GET /api/v1/subs"`, `AddSubscriptionsPattern = "POST /api/v1/subs"`, `RemoveSubscriptionPattern = "DELETE /api/v1/subs/{id}"`, `PullRequestsPattern = "GET /api/v1/subs/prs"`, `SubscriptionIDPathValue = "id"`, `SinceQueryParameter = "since"`.
  - `ErrorCode`: `unauthorized`, `invalid_body`, `unknown_repository`, `too_many_subscriptions`, `invalid_since`, `not_found`, `internal_error`.
  - Errors: `ErrSubscriptionNotFound`, `ErrTooManySubscriptions`, `ErrNoRepositories`, `*UnknownRepositoriesError{Repositories []string}`.
  - Ports: `SubscriptionStore{List(ctx, tokenID) ([]Subscription, error); Add(ctx, NewSubscriptions) (int, error); Delete(ctx, tokenID string, id int64) (Subscription, error)}`; `PullRequestStore{Upsert(ctx, PullRequest) error; Delete(ctx, repository string, number int) error; Repositories(ctx) ([]string, error); ChangedSince(ctx, repositories []string, since time.Time) ([]PullRequest, error)}`; `DeliveryStore{Claim(ctx, Claim) ([]PullRequest, error)}`; `RepositoryCatalog{IsConfigured(repository string) bool; Configured(repositories []string) []string}`; `Clock{Now() time.Time}`.
  - Use cases: `Subscriptions{List(ctx, Subscriber) ([]Subscription, error); Add(ctx, Subscriber, repositories []string) ([]Subscription, error); Remove(ctx, Subscriber, id int64) error}`; `PullRequests{Next(ctx, Subscriber) ([]PullRequest, error); Since(ctx, Subscriber, since time.Time) ([]PullRequest, error)}`; `Recorder{Record(ctx, kernel.Event) error}`.
  - Constructors (`feed/application`): `NewRecorder(pullRequests domain.PullRequestStore, catalog domain.RepositoryCatalog, clock domain.Clock) *Recorder`, `NewSubscriptions(domain.SubscriptionsParams) *Subscriptions`, `NewPullRequests(domain.PullRequestsParams) *PullRequests`.
  - `routingdomain.EntryLister{Entries() []Entry}`, satisfied by `*routingapp.Provider`.

- [ ] **Step 1: Write the failing tests** (in-memory fakes; a fake clock at `2026-10-28T09:00:00Z`; a `slog` text logger into a buffer)

  - `TestRecorder_Record` table: `opened`, `ready_for_review`, `updated`, `reopened` → one upsert; `closed`, `merged`, `converted_to_draft` → one delete of (`acme/api`, 42); `approved`, `commented`, `unknown` → nothing.
  - `TestRecorder_SkipsUnconfiguredRepository`: catalog says no → no store call, nil error.
  - `TestRecorder_StoresFeedFields`: event `Acme/API`, PR with `Body`, `HeadSHA`, `AuthorID`, branches, `CreatedAt` in `+02:00`, `Author: "octocat"`, `Sender.Login: "octocat"` → the upserted `PullRequest` has `Repository "acme/api"`, `Description` = body, the four fields, `CreatedAt` in UTC, `UpdatedAt` = clock now; `fmt.Sprintf("%+v", stored)` does not contain `octocat` (no author name).
  - `TestRecorder_ReadyForReviewRefreshesHeadSHA`: two `ready_for_review` events with different `HeadSHA` → the second upsert carries the new SHA.
  - `TestRecorder_ReturnsStoreError`: upsert fails → the error wraps it.
  - `TestSubscriptions_Add` table (catalog fake configures `acme/api`, `acme/web` and the tier `acme/*` for `acme/ops`): exact repo; repo via `owner/*`; `"*"`; `ACME/Api` stored as `acme/api`; duplicates in one request add once; an already-subscribed repo is kept with no error; returns the full list.
  - `TestSubscriptions_AddRejects` table — each asserts `store.Add` was **not** called: unknown repos → `*UnknownRepositoriesError{Repositories: ["acme/nope", "other/x"]}` (lower case, sorted); empty list → `ErrNoRepositories`; 199 existing + 2 new → `ErrTooManySubscriptions`. And 199 existing + 1 new + 1 already subscribed → accepted.
  - `TestSubscriptions_AddLogs`: newly added 2 → `feed subscriptions added` with `user`, `token_id`, `count=2`; all already subscribed → no such line.
  - `TestSubscriptions_Remove`: own id → nil and `feed subscription removed` with `repository=acme/api`; store returns `ErrSubscriptionNotFound` → `errors.Is` holds, no log line.
  - `TestPullRequests_Next` table: subscribed `acme/api` + unconfigured `old/gone` → `Claim.Repositories == [acme/api]`; `"*"` with `PullRequestStore.Repositories` returning `acme/api, acme/new` (added later) → both claimed; no subscriptions → empty result and `Claim` not called; subscriptions all unconfigured → `Claim` not called. `Claim.DeliveredAt` = clock now, `TokenID` from the subscriber.
  - `TestPullRequests_NextLogs`: 3 claimed → `feed pull requests delivered` with `count=3`; 0 → no line.
  - `TestPullRequests_Since`: same repo resolution; `ChangedSince` receives `since` in UTC; `Claim` is never called.

- [ ] **Step 2: Run them and see them fail**

  Run: `go test ./internal/feed/...`
  Expected: FAIL (package does not exist).

- [ ] **Step 3: Implement** the contracts and the three use cases. `Recorder` upsert kinds: `opened`, `ready_for_review`, `updated`, `reopened`; delete kinds: `closed`, `merged`, `converted_to_draft`. `PullRequests` resolves repos as: the token's subscriptions → if any is `"*"`, add `PullRequestStore.Repositories` → `Catalog.Configured(...)`.

- [ ] **Step 4: Run the tests and see them pass**, then **run the task gate** (it checks the new depguard rules).

- [ ] **Step 5: Commit**

  ```bash
  git add internal/feed internal/routing .golangci.yml
  git commit -m "feat(feed): domain contracts, recorder and subscription use cases"
  ```

---

### Task 4: Feed store adapters, routing catalog and clock

**Files:**
- Create: `internal/feed/infrastructure/{doc.go,subscription_repo.go,pull_request_repo.go,delivery_repo.go,routing_catalog.go,system_clock.go}`
- Test: `internal/feed/infrastructure/repos_test.go`, `routing_catalog_test.go`

**Interfaces:**
- Consumes: Task 2 persistence types; Task 3 ports; `routingdomain.EntryLister`.
- Produces: `NewSubscriptionRepo(*persistence.FeedSubscriptions) *SubscriptionRepo` (→ `SubscriptionStore`; maps `persistence.ErrNotFound` to `domain.ErrSubscriptionNotFound`), `NewPullRequestRepo(*persistence.FeedPullRequests) *PullRequestRepo` (→ `PullRequestStore`; `CreatedAt` zero ↔ `nil`), `NewDeliveryRepo(*persistence.FeedDeliveries) *DeliveryRepo` (→ `DeliveryStore`), `NewRoutingCatalog(routingdomain.EntryLister) *RoutingCatalog` (→ `RepositoryCatalog`), `NewSystemClock() *SystemClock` (→ `Clock`).

- [ ] **Step 1: Write the failing tests**

  - `TestRoutingCatalog_IsConfigured` table, entries `Acme/API` (explicit), `acme/*` (wildcard for org `acme`), `beta/svc` (explicit): `acme/api` yes; `ACME/Api` yes; `acme/anything` yes (wildcard); `beta/svc` yes; `beta/other` no; `other/x` no; `acme` no; `acme/` no; `*` no.
  - `TestRoutingCatalog_Configured`: `[BETA/svc, other/x, acme/api, beta/svc]` → `[beta/svc, acme/api]` (lower case, order kept, unconfigured and duplicates dropped).
  - `repos_test.go` on `persistence.NewTestDB` (seed a token): a `PullRequest` round-trips every field through `Upsert` + `ChangedSince`; zero `CreatedAt` round-trips as zero; `SubscriptionRepo.Delete` of an unknown id → `errors.Is(err, domain.ErrSubscriptionNotFound)`; `DeliveryRepo.Claim` returns domain `PullRequest`s.

- [ ] **Step 2: Run them and see them fail** — `go test ./internal/feed/infrastructure/...` → FAIL (undefined).

- [ ] **Step 3: Implement.** The catalog builds, once in the constructor, a set of lower-cased `org/repo` for explicit entries and a set of lower-cased orgs for wildcard entries; a name matches when it splits into a non-empty `org` and `repo` at the first `/` and is in the first set or its org is in the second.

- [ ] **Step 4: Run the tests and see them pass**, then **run the task gate**.

- [ ] **Step 5: Commit**

  ```bash
  git add internal/feed/infrastructure
  git commit -m "feat(feed): store adapters and routing catalog"
  ```

---

### Task 5: HTTP adapters for `/api/v1/subs` and `/api/v1/subs/prs`

**Files:**
- Create: `internal/feed/infrastructure/{subs_handler.go,prs_handler.go,routes.go}`
- Test: `internal/feed/infrastructure/subs_handler_test.go`, `prs_handler_test.go`, `http_fakes_test.go`

**Interfaces:**
- Consumes: `accessdomain.PrincipalFrom`, `httpx.Route`, `httpx.WriteJSON`, `httpx.WriteError`, `httpx.WriteNoContent`; Task 3 use cases and constants.
- Produces: `NewRoutes(subscriptions domain.Subscriptions, pullRequests domain.PullRequests, logger *slog.Logger) []httpx.Route` — the four patterns from Task 3.

- [ ] **Step 1: Write the failing tests** (register the routes on an `http.ServeMux`; put a `Principal{TokenID: "t1", User: "alice"}` in the request context with `accessdomain.WithPrincipal`; fakes record their inputs)

  - `GET /api/v1/subs` → `200`, `JSONEq` `{"subscriptions":[{"id":17,"repository":"acme/api","created_at":"2026-10-28T09:00:00Z"}]}`; none → `{"subscriptions":[]}`; the fake received `Subscriber{TokenID: "t1", User: "alice"}`.
  - `POST /api/v1/subs` `{"repositories":["acme/api","acme/web"]}` → `200` with the full list (same shape as GET) and the fake received both repos.
  - `POST` bad requests table → `400 {"error":"invalid_body"}`: not JSON; `{"repositories":["acme/api"],"extra":1}`; a body over 16 KiB (valid JSON); `{"repositories":[]}` (use case returns `ErrNoRepositories`).
  - `POST` with `*UnknownRepositoriesError{[acme/nope]}` → `400`, `JSONEq` `{"error":"unknown_repository","repositories":["acme/nope"]}`; `ErrTooManySubscriptions` → `400 {"error":"too_many_subscriptions"}`.
  - `DELETE /api/v1/subs/17` → `204`, fake got id `17`; `ErrSubscriptionNotFound` → `404 {"error":"not_found"}`; ids `abc`, `0`, `-1` → `404 not_found` and the use case is not called.
  - `GET /api/v1/subs/prs` → `200`, `JSONEq` with the spec's example object (all eleven fields); none → `{"pull_requests":[]}`; zero `CreatedAt` → `"created_at":null`; `Since` not called.
  - `?since=2026-10-28T11:00:00+02:00` → `Since` called with an instant `Equal` to `2026-10-28T09:00:00Z`; `Next` not called.
  - `?since=yesterday` and `?since=` → `400 {"error":"invalid_since"}`, no use case call.
  - Any use case returning a plain error → `500 {"error":"internal_error"}` and the log has `agent api request failed`.
  - A request with no `Principal` → `401 {"error":"unauthorized"}`.
  - `TestNewRoutes_Patterns`: exactly the four patterns.

- [ ] **Step 2: Run them and see them fail** — `go test ./internal/feed/infrastructure/... -run 'Subs|Prs|Routes'` → FAIL.

- [ ] **Step 3: Implement** per Decisions 4–8 and the Interfaces block. Reuse PR 1's time format (RFC 3339, UTC, seconds).

- [ ] **Step 4: Run the tests and see them pass**, then **run the task gate**.

- [ ] **Step 5: Commit**

  ```bash
  git add internal/feed/infrastructure
  git commit -m "feat(feed): subscription and pull request HTTP adapters"
  ```

---

### Task 6: `feed` fx module, recording fan-out and runtime wiring

**Files:**
- Create: `internal/feed/module.go`, `internal/feed/module_test.go`, `internal/runtime/feed.go`, `internal/runtime/feed_test.go`, `internal/runtime/integration_feed_test.go`
- Modify: `internal/runtime/module.go` (`feed.Module`; `persistence.NewFeedSubscriptions`, `NewFeedPullRequests`, `NewFeedDeliveries`; `provideEntryLister`; `buildWebhookDispatcher`; `fx.Provide(AsAgentAPIRoutes(feedinfra.NewRoutes))`; `buildMux` and `registerWebhookRoute` take `notificationdomain.EventDispatcher`)

**Interfaces:**
- Consumes: everything above; PR 1's `AsAgentAPIRoutes`, `agentAPIConfig`, `buildAgentAPIApp`, `request`; `newSlackFake`, `seedsToMappings`, `primeLock`, `githubSignature` from `integration_test.go`.
- Produces: `feed.Module` (binds the five adapters to their ports and the three use cases; expects `*persistence.FeedSubscriptions`, `*persistence.FeedPullRequests`, `*persistence.FeedDeliveries`, `routingdomain.EntryLister`, `*slog.Logger`); `buildWebhookDispatcher(cfg config.Config, dispatcher *notificationapp.Dispatcher, recorder feeddomain.Recorder, logger *slog.Logger) notificationdomain.EventDispatcher` (plain dispatcher when disabled, `recordingDispatcher` when enabled).

- [ ] **Step 1: Write the failing tests**

  - `feed/module_test.go`: an fx app with `feed.Module` and the supplied inputs resolves `domain.Subscriptions`, `domain.PullRequests`, `domain.Recorder` (`app.Err()` is nil).
  - `runtime/feed_test.go`:
    - `TestRecordingDispatcher_RecordErrorKeepsDispatchResult`: next → nil, recorder → error → `Dispatch` returns nil; log has `feed pull request not recorded` with `repository=acme/api`, `pr=42`, `kind=updated`, `err=`.
    - `TestRecordingDispatcher_RecordsWhenDispatchFails`: next → `errSlack` → recorder was called and `Dispatch` returns `errSlack`.
    - `TestRecordingDispatcher_RecordsWithoutCancel`: a cancelled ctx → the recorder saw `ctx.Err() == nil`.
    - `TestBuildWebhookDispatcher_DisabledReturnsPlainDispatcher`: disabled → the result is the given `*notificationapp.Dispatcher`.
  - `runtime/integration_feed_test.go` `TestIntegration_AgentFeedFlow` (config = `agentAPIConfig` + a mapping for `acme/api` with a primed lock and the Slack fake; logger replaced by a debug text logger into a buffer):
    admin creates invite → redeem → `POST /api/v1/subs {"repositories":["Acme/API"]}` → `200` lists `acme/api` → signed GitHub `opened` for `acme/api` #42 (head sha `aaa…`) → `GET /api/v1/subs/prs` lists #42 with `head_sha` `aaa…` → `GET` again → empty → signed `synchronize` (head sha `bbb…`) → `GET` → empty → `GET ?since=<time before opened>` lists #42 with `head_sha` `bbb…` → `GET` → still empty (since recorded nothing) → signed `closed` → `GET ?since=…` → empty → signed `reopened` → `GET` lists #42 again → `DELETE /api/v1/subs/{id}` → `204` → admin `DELETE /admin/v1/tokens/{id}` → `GET /api/v1/subs/prs` → `401`. Finally the log buffer does not contain the token or its secret part.
  - `TestIntegration_AgentAPIDisabled_NothingTracked`: API off, a mapped repo, a signed `opened` → `200`; `GET /api/v1/subs` → `404`; `feed_pull_requests` has 0 rows.

- [ ] **Step 2: Run them and see them fail** — `go test -race ./internal/feed/ ./internal/runtime/... -run 'Module|Recording|WebhookDispatcher|Feed|NothingTracked'` → FAIL.

- [ ] **Step 3: Implement** `feed.Module` (template: `internal/access/module.go`), `recordingDispatcher` and `buildWebhookDispatcher` in `runtime/feed.go` (Decision 1), and the `module.go` changes.

- [ ] **Step 4: Run the tests and see them pass**, then **run the task gate**.

- [ ] **Step 5: Commit**

  ```bash
  git add internal/feed internal/runtime
  git commit -m "feat(runtime): record PRs for the feed and serve the subscription routes"
  ```

---

### Task 7: Docs

**Files:**
- Create: `docs/agent-api.md`
- Modify: `mkdocs.yml` (nav **Use**: `Agent API: agent-api.md`), `docs/security.md`, `docs/operations.md`, `docs/configuration.md`, `README.md`, `ARCHITECTURE.md`, `CLAUDE.md`

- [ ] **Step 1: Write `docs/agent-api.md`** (simple English, short sentences, match the other pages): what it is (peer review, polling, no code served); enabling it (link to `configuration.md` and `security.md#agent-api-access` for the admin port, keygen and invites — do not copy them); the agent setup from the spec (`NOTIFYCAT_URL`, `NOTIFYCAT_TOKEN`, header on stdin); every `/api/v1/subs*` route with request, response and error codes from this plan; delivery rules (once per token, shared token = shared list, close/draft then reopen/ready returns it, new commits do not, `?since=` re-reads and records nothing, how to recover a lost list); a Claude Code `/loop 5m` prompt and the "read the diff, post the review" step for GitHub (`gh pr diff`, `gh pr review`) and Bitbucket (REST `GET …/pullrequests/{id}/diff`, `POST …/comments`); poll every minute or slower; limits (200 subscriptions, 16 KiB body, PRs missed during downtime, PRs already open before v1 and never touched again).

- [ ] **Step 2: Update the other pages**
  - `docs/security.md`: drop "The agent feed endpoints follow in a later release." and the "Once the agent feed ships, " prefix; link to `agent-api.md`.
  - `README.md:23`: drop "The agent PR feed follows in a later release."; mention the open-PR feed and link `docs/agent-api.md`.
  - `docs/operations.md`: the four feed log lines from the spec (`feed subscriptions added`, `feed subscription removed`, `feed pull requests delivered`, `feed pull request not recorded`), and `kind=updated` / `kind=reopened` as new `no_handler` kinds.
  - `docs/configuration.md` `agent_api.enabled`: when on, notifycat also records open PRs of configured repos for the feed.
  - `ARCHITECTURE.md`: a `feed` domain node and edges (`feed --> kernel`, `feed --> routing`, `feed --> access`, `feed --> platform`), the domain table row, the request flow, the `notifycat-server` row; nine domains.
  - `CLAUDE.md`: "Nine domains", a `feed` row, the `/api/v1/subs*` request-flow lines, and the three `feed_*` tables in the Persistence paragraph.

- [ ] **Step 3: Verify the docs build**

  Run: `docker run --rm -v "$PWD":/docs squidfunk/mkdocs-material:9.5.39 build --strict --site-dir /tmp/site && rm -rf .cache`
  Expected: exit 0, no warnings.

- [ ] **Step 4: Run the task gate.**

- [ ] **Step 5: Commit**

  ```bash
  git add docs mkdocs.yml README.md ARCHITECTURE.md CLAUDE.md
  git commit -m "docs(feed): agent API guide, logs and architecture"
  ```
