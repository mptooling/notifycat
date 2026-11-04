# Review parties for shared repositories: implementation plan (PR 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Agents that subscribe to a repository as `party` share its PRs: each PR goes to at most `agent_api.party_reviewers` party members, members take turns by daily load, `agent_api.party_deadline` ends the turns, and no feed PR is delivered on days off.

**Architecture:** The `feed` domain gets a subscription type, a turn rule (a pure application use case behind `domain.TurnPolicy`), a work calendar (an application use case over the digest calendar), poll tracking and an atomic party claim. Persistence stays domain-agnostic: `FeedDeliveries.ClaimParty` loads a snapshot inside one SQLite write transaction and asks a `Choose` callback which PRs to deliver; the feed `DeliveryRepo` bridges that callback to `domain.TurnPolicy`. The recorder stores the cap, the first record time and the timer's end instant with each PR. Migration `00012` adds the columns and two tables. Only global settings in this PR; per-repository and per-path settings are PR 2.

**Tech Stack:** Go 1.27.3, uber/fx, GORM + `github.com/glebarez/sqlite` (modernc SQLite: `RETURNING`, `DROP COLUMN`), goose v3, testify.

**Spec:** `docs/superpowers/specs/2026-11-03-feed-review-party-design.md`, "Delivery in 2 PRs" item 1: everything except the tier and path keys. PR 2 owns calls 2, 13, 16 and 19, and the tier and path levels of calls 1, 12 and 18.

## Global Constraints

- DDD + hexagonal + uber/fx per `CLAUDE.md`: every use case and every infrastructure service has an interface in `internal/feed/domain/interfaces.go`; enums in `enums.go`, constants in `constants.go`; more than 3 arguments (counting `ctx`) to an exported func or constructor → a DTO; doc comments on interfaces only; one `fx.Module` per domain.
- `feed/domain` imports only the kernel and the standard library. `feed/application` imports only `feed/domain` and the kernel. `feed/infrastructure` may import `access/domain`, `digest/domain`, `routing/domain`, `platform/persistence`, `platform/httpx`. `platform/persistence` never imports a domain.
- Readable names (no `m`, `d`, `tc`); table tests use `testCases`/`testCase`.
- testify; `(t, want, got)`; arrange / act / assert; `require` for setup, `assert` for final checks.
- Minimal comments. None that restate code.
- Conventional Commits. Never mention AI or Claude in commits. No `Co-Authored-By` trailer. Never the literal string `BREAKING CHANGE`. PR title and squash commit: `feat(feed): review parties for shared repositories`.
- Foreign keys are ON on every connection. A migration never rebuilds a parent table. Upserts use `ON CONFLICT … DO UPDATE`, never `INSERT OR REPLACE`.
- Every transaction that reads then writes starts with a write statement, so SQLite never upgrades a read lock.
- Repository names are lower case everywhere in the feed.
- Tokens and `Authorization` headers never reach logs. Feed logs name `user` and `token_id` only.
- Party settings never change a Slack notification. This PR does not touch `internal/routing/application`, `internal/notification` or `config.lock` code.
- Log messages and fields exactly as the spec's "Logs" table, plus ruling 10.
- Values: `party_reviewers` default `1`, integer ≥ 1. `party_deadline` default `1h`, format hours and minutes (`15m`, `2h`, `1h30m`), from `15m` to `24h` inclusive. Away window `24h` of clock time. Load = party deliveries per (token, repository) per day; the day is the date in `digest.timezone` (UTC when unset).
- **Task gate** (the "run the task gate" step in each task):

  ```bash
  go vet ./... && go test -race ./... && go build ./...
  docker run --rm -v "$PWD":/app -w /app -v "$(go env GOMODCACHE)":/go/pkg/mod golangci/golangci-lint:v2.14.0 golangci-lint run ./...
  ```

  Expected: every command exits 0.

## Rulings (decisions the spec leaves to the builder)

1. **The timer is stored as an instant.** `feed_pull_requests.party_deadline_at` = `WorkCalendar.AddWorkingTime(recorded_at, deadline)`, computed by the recorder on every recorded event. The spec suggested `party_deadline_minutes`; an instant keeps the calendar out of the atomic claim. A NULL `party_deadline_at` (rows from before `00012`) means the timer ran out.
2. **Old load rows go inside the claim.** The first statement of every party claim is `DELETE FROM feed_party_loads WHERE day < ?`. It also takes the SQLite write lock first. This replaces the spec's suggested startup + 24-hour job: no new goroutine.
3. **Turn rule wiring.** `application.TurnPolicy` implements `domain.TurnPolicy`. `persistence.FeedPartyClaim` carries a `Choose func(FeedPartySnapshot) []FeedPullRequestKey`. `infrastructure.DeliveryRepo` gets the policy by injection and builds the callback.
4. **Party setting parsing lives in `routing/domain`.** `config` already imports it, and PR 2's tier and path keys reuse it.
5. **The feed's day-off calendar** is its own `digestapp.Calendar` built with a nil logger, so the "holidays not configured" warning is not logged a third time.
6. **`PATCH /api/v1/subs`.** Missing or empty `type` → `400 invalid_type`. `"repositories": []` → `400 invalid_body`. Entries are trimmed, lower-cased and de-duplicated like `POST`. `"*"` is allowed. `not_subscribed` lists the missing entries lower-case and sorted, same body shape as `unknown_repository`. On any error nothing changes.
7. **Poll record.** `Next` records the poll after both claims succeed, and on a day off. A failed claim records nothing.
8. **`?since=` on a party repository** returns PRs with any delivery row for the token (solo or party).
9. **Away check** is SQL: a member is a candidate when `feed_polls.polled_at >= ActiveSince`, `ActiveSince = now - 24h`.
10. **New log** `feed subscriptions changed` (info): `user`, `token_id`, `type`, `count`. Only when `count > 0`.
11. **PR 2's plan** is written after this PR merges, from the merged code.

## Review Focus

1. Feed integration tests poll with the real clock. On Saturday or Sunday they would get empty lists and fail. Every feed integration test that polls must pin a weekday clock (Task 9).
2. `config.Config` built by hand in tests has `DigestTimezone == nil`. The feed must then use UTC and never panic (Task 3).
3. `TestMigration00010_DownDropsAccessTables` and `TestMigration00011_DownDropsFeedTables` step down one migration per call. With `00012` they need one more `MigrateDown` (Task 4).
4. Two party members that poll at the same moment must never pass the cap (Task 6 race test).
5. `PATCH` with `"Acme/API"` must change the stored `acme/api` subscription, not answer `not_subscribed` (Task 8).

---

### Task 1: `agent_api.party_reviewers` and `agent_api.party_deadline`

**Files:**
- Modify: `internal/routing/domain/constants.go`
- Create: `internal/routing/domain/party.go`, `internal/routing/domain/party_test.go`
- Modify: `internal/platform/config/config.go`, `internal/platform/config/config_test.go`

**Interfaces:**
- Produces: `routingdomain.DefaultPartyReviewers = 1`, `DefaultPartyDeadline = time.Hour`, `MinPartyDeadline = 15 * time.Minute`, `MaxPartyDeadline = 24 * time.Hour`, `PartyDeadlinePattern = "^(?:[0-9]+h)?(?:[0-9]+m)?$"`; `func ParsePartyDeadline(value string) (time.Duration, error)`; `func ValidatePartyReviewers(value int) error`; `config.Config.PartyReviewers int`, `config.Config.PartyDeadline time.Duration`.

- [ ] **Step 1: Write the failing tests**

`TestParsePartyDeadline` (table): `"15m"`→15m, `"2h"`→2h, `"1h30m"`→90m, `"24h"`→24h accepted; `"14m"`, `"25h"`, `"24h1m"`, `"30s"`, `"60s"`, `"1h30m0s"`, `"1.5h"`, `"90"`, `"-1h"`, `""` each return an error.
`TestValidatePartyReviewers`: 1 and 5 → nil; 0 and -1 → error.
Config: `TestLoad_AgentAPI_PartyDefaults` (no keys → `1`, `time.Hour`); `TestLoad_AgentAPI_PartySettings` (`party_reviewers: 3`, `party_deadline: 1h30m` → `3`, `90*time.Minute`); `TestLoad_AgentAPI_PartyReviewersBelowOne` (`0` → error containing `agent_api.party_reviewers`); `TestLoad_AgentAPI_PartyDeadlineOutOfRange` (`14m` → error containing `agent_api.party_deadline`).

- [ ] **Step 2: Run the tests, expect FAIL** (`go test ./internal/routing/domain/... ./internal/platform/config/...`: undefined symbols).
- [ ] **Step 3: Implement.** `ParsePartyDeadline` matches the pattern (non-empty), then `time.ParseDuration`, then the range check. Errors name the value and the rule (`use hours and minutes, like 15m, 2h or 1h30m`; `must be from 15m to 24h`). Config: `fileSchema.AgentAPI` gets `PartyReviewers *int \`yaml:"party_reviewers"\`` and `PartyDeadline string \`yaml:"party_deadline"\``; `defaults()` sets both defaults; `Load` validates and wraps as `config: agent_api.party_reviewers: …` / `config: agent_api.party_deadline: …`. Validate also when the agent API is off.
- [ ] **Step 4: Run the tests, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(config): agent_api party_reviewers and party_deadline`.

### Task 2: Turn rule

**Files:**
- Modify: `internal/feed/domain/models.go`, `internal/feed/domain/interfaces.go`
- Create: `internal/feed/application/turn_policy.go`, `internal/feed/application/turn_policy_test.go`
- Modify: `internal/feed/module.go`

**Interfaces:**
- Produces (domain models):
  ```go
  type PullRequestKey struct { Repository string; Number int }
  type PartyState struct { Reviewers int; RecordedAt time.Time; DeadlineAt time.Time } // zero DeadlineAt: the timer ran out
  type PartyMember struct { TokenID string; Repository string }
  type WaitingPullRequest struct {
      Key             PullRequestKey
      Party           PartyState
      PartyDeliveries int
      Candidates      []string // other members of the repository, not away, without a delivery of this PR
  }
  type PartyRound struct { TokenID string; Now time.Time; PullRequests []WaitingPullRequest; Loads map[PartyMember]int }
  type PartyDecision struct { Take []PullRequestKey; Taken int; Waiting int }
  ```
  `domain.TurnPolicy interface { Choose(round PartyRound) PartyDecision }`; `application.NewTurnPolicy() *TurnPolicy`.

- [ ] **Step 1: Write the failing test** `TestTurnPolicy_Choose` (table, `Now` = Tue 2026-11-03 10:00 UTC, cap 1 and a deadline in the future unless stated):
  - nobody else → take.
  - `PartyDeliveries == Reviewers` → not taken, `Taken: 1`.
  - poller load 3, candidate `bob` load 1 → `Waiting: 1`.
  - equal loads → take.
  - cap 3, two candidates with lower load → take.
  - cap 2, `PartyDeliveries` 0, two candidates with lower load → `Waiting: 1`.
  - cap 2, `PartyDeliveries` 1, one candidate with lower load → `Waiting: 1`.
  - lower-load candidate, `DeadlineAt == Now` → take (the timer ran out).
  - lower-load candidate, zero `DeadlineAt` → take.
  - two PRs of one repository, poller and `bob` both load 0, cap 1 → first taken, second `Waiting: 1` (the load rises inside one round).
  - poller load 5 in `acme/web`, 0 in `acme/api`; `bob` load 1 in both → `acme/api` PR taken, `acme/web` PR waits.
  - `Take` keeps the input order.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement `(*TurnPolicy).Choose`.** Copy `round.Loads`. For each PR in order: at cap → `Taken++`; else when `Now.Before(DeadlineAt)` count candidates with load lower than the poller's load in that repository; when that count ≥ `Reviewers - PartyDeliveries` → `Waiting++`; else take it and add 1 to the poller's load in that repository. Register `fx.Annotate(application.NewTurnPolicy, fx.As(new(domain.TurnPolicy)))` in `feed.Module`.
- [ ] **Step 4: Run, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(feed): party turn rule`.

### Task 3: Work calendar

**Files:**
- Modify: `internal/feed/domain/interfaces.go`, `models.go`, `constants.go`
- Create: `internal/feed/application/work_calendar.go`, `work_calendar_test.go`
- Create: `internal/feed/infrastructure/digest_day_off_calendar.go`, `digest_day_off_calendar_test.go`
- Modify: `internal/feed/module.go`, `internal/feed/module_test.go`, `internal/runtime/feed.go`, `internal/runtime/feed_test.go`, `internal/runtime/module.go`

**Interfaces:**
- Produces:
  ```go
  // domain
  type WorkCalendar interface {
      IsDayOff(moment time.Time) bool
      Day(moment time.Time) string            // YYYY-MM-DD in the team's timezone
      AddWorkingTime(start time.Time, workingTime time.Duration) time.Time
  }
  type DayOffCalendar interface { IsDayOff(localMoment time.Time) bool }
  type PartySettings struct { Reviewers int; Deadline time.Duration }
  type Settings struct { Party PartySettings; Location *time.Location }
  type WorkCalendarParams struct { DaysOff DayOffCalendar; Location *time.Location }
  const DayLayout = "2006-01-02"
  const MaxWorkingTimeSearchDays = 31
  // application
  func NewWorkCalendar(params domain.WorkCalendarParams) *WorkCalendar  // nil Location → time.UTC
  // infrastructure
  func NewDigestDayOffCalendar(calendar digestdomain.DigestCalendar) *DigestDayOffCalendar
  // runtime
  func provideFeedSettings(cfg config.Config) feeddomain.Settings          // nil DigestTimezone → time.UTC
  func provideFeedDayOffCalendar(provider *routingapp.Provider) feeddomain.DayOffCalendar
  ```

- [ ] **Step 1: Write the failing tests.** `TestWorkCalendar_AddWorkingTime` (table, UTC, fake `DayOffCalendar` = weekends, plus Wed 2026-11-04 off where stated):
  - Tue 10:00 + 30m → Tue 10:30.
  - Fri 2026-11-06 23:50 + 30m → Mon 2026-11-09 00:20.
  - Sat 12:00 + 1h → Mon 01:00.
  - Fri 20:00 + 24h → Mon 20:00.
  - Tue 23:00 + 2h with Wed off → Thu 01:00.
  - every day off → `start.Add(workingTime)` (search limit).
  `TestWorkCalendar_UsesTheLocation`: Location `Europe/Berlin`; Fri 2026-11-06 22:50 UTC (23:50 Berlin) + 30m → Sun 23:20 UTC (Mon 00:20 Berlin); `IsDayOff` of Sat 2026-11-07 00:30 Berlin given as Fri 23:30 UTC → true; `Day` of that moment → `"2026-11-07"`.
  `TestWorkCalendar_NilLocationIsUTC`.
  `TestDigestDayOffCalendar_IsDayOff`: weekday false, Saturday true, with country `DE` 2026-12-25 true.
  `TestProvideFeedSettings_NilTimezoneIsUTC` in `internal/runtime/feed_test.go`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.** `AddWorkingTime` walks day by day in `Location`: a day off moves to the next local midnight (`time.Date(year, month, day+1, 0, 0, 0, 0, location)`); a working day uses up the time until its next midnight; stop when the remaining time fits; after `MaxWorkingTimeSearchDays` days return `start.Add(workingTime)`. `IsDayOff` and `Day` convert with `moment.In(location)`. `DigestDayOffCalendar.IsDayOff` returns the `skip` of `SkipReason`. Runtime: `provideFeedSettings` copies `cfg.PartyReviewers`, `cfg.PartyDeadline`, `cfg.DigestTimezone`; `provideFeedDayOffCalendar` wraps `digestapp.NewCalendar(digestdomain.CalendarParams{Country: digestCountry(provider)})` (nil logger, ruling 5). `feed.Module` provides `domain.WorkCalendar` from `Settings.Location` and `DayOffCalendar`. Add both runtime providers to `runtime.Module`; supply both in `feed/module_test.go`.
- [ ] **Step 4: Run, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(feed): work calendar for days off and the review timer`.

### Task 4: Migration `00012`, subscription type and polls in persistence

**Files:**
- Create: `internal/platform/persistence/migrations/00012_feed_party.sql`
- Modify: `internal/platform/persistence/models.go`, `feed.go`, `feed_test.go`, `access_test.go`
- Modify: `internal/runtime/module.go` (provide `persistence.NewFeedPolls`), `internal/feed/module_test.go`

**Interfaces:**
- Produces: `FeedSubscription.Type string \`gorm:"column:type;not null;default:solo"\``; `FeedSubscriptionBatch.Type string`; `FeedTypeChange{TokenID string; Repositories []string /* nil: every subscription */; Type string}`; `(*FeedSubscriptions).SetType(ctx, change FeedTypeChange) (int, error)`; `FeedPoll{TokenID, PolledAt}` (table `feed_polls`); `NewFeedPolls(db) *FeedPolls`; `(*FeedPolls).Record(ctx, tokenID string, polledAt time.Time) error`; `FeedPartyLoad{TokenID, Repository, Day string; Taken int}` (table `feed_party_loads`).

- [ ] **Step 1: Write the failing tests.**
  - `TestMigration00012_DownRemovesPartyTablesAndColumns`: after one `MigrateDown`, `feed_polls` and `feed_party_loads` are gone and `pragma_table_info('feed_subscriptions')` has no `type`; `MigrateUp` brings them back.
  - Add one `MigrateDown` (comment `// 00012`) to `TestMigration00010_DownDropsAccessTables` and `TestMigration00011_DownDropsFeedTables`.
  - `TestFeedSubscriptions_AddStoresTypeAndDefaultsToSolo`: batch with `Type: "party"` → listed `party`; batch with empty `Type` → `solo`.
  - Extend `TestFeedSubscriptions_AddKeepsExisting`: a second `Add` of a stored repository with `Type: "party"` keeps the stored `solo` (spec call 8).
  - `TestFeedSubscriptions_SetTypeChangesOnlyTheNamedRows`: `t1` has `acme/api`, `acme/web`, `*`; change `["acme/api","*"]` to party → returns 2; `acme/web` stays solo; `t2`'s `acme/api` stays solo.
  - `TestFeedSubscriptions_SetTypeWithoutRepositoriesChangesEveryRow`.
  - `TestFeedPolls_RecordUpserts`: two records → one row with the later time.
  - Extend `TestFeedTables_TokenDeleteCascades` to `feed_polls` and `feed_party_loads`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.** Migration, exactly:

  ```sql
  -- +goose Up
  ALTER TABLE feed_subscriptions ADD COLUMN type TEXT NOT NULL DEFAULT 'solo';
  ALTER TABLE feed_pull_requests ADD COLUMN party_reviewers INTEGER NOT NULL DEFAULT 1;
  ALTER TABLE feed_pull_requests ADD COLUMN recorded_at DATETIME;
  ALTER TABLE feed_pull_requests ADD COLUMN party_deadline_at DATETIME;
  ALTER TABLE feed_deliveries ADD COLUMN party BOOLEAN NOT NULL DEFAULT 0;

  CREATE TABLE feed_party_loads (
      token_id   TEXT    NOT NULL REFERENCES access_tokens(id) ON DELETE CASCADE,
      repository TEXT    NOT NULL,
      day        TEXT    NOT NULL,
      taken      INTEGER NOT NULL,
      PRIMARY KEY (token_id, repository, day)
  );

  CREATE TABLE feed_polls (
      token_id  TEXT     PRIMARY KEY REFERENCES access_tokens(id) ON DELETE CASCADE,
      polled_at DATETIME NOT NULL
  );

  -- +goose Down
  DROP TABLE feed_polls;
  DROP TABLE feed_party_loads;
  ALTER TABLE feed_deliveries DROP COLUMN party;
  ALTER TABLE feed_pull_requests DROP COLUMN party_deadline_at;
  ALTER TABLE feed_pull_requests DROP COLUMN recorded_at;
  ALTER TABLE feed_pull_requests DROP COLUMN party_reviewers;
  ALTER TABLE feed_subscriptions DROP COLUMN type;
  ```

  `Delete`'s `RETURNING` also returns `type`. `SetType` is one `UPDATE … WHERE token_id = ? [AND repository IN ?]`. `Record` is `ON CONFLICT (token_id) DO UPDATE SET polled_at = excluded.polled_at`, UTC.
- [ ] **Step 4: Run, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(persistence): feed party migration, subscription type and polls`.

### Task 5: Party columns on feed pull requests

**Files:**
- Modify: `internal/platform/persistence/models.go`, `feed.go`, `feed_test.go`

**Interfaces:**
- Produces: `FeedPullRequest.PartyReviewers int \`gorm:"column:party_reviewers;not null;default:1"\``, `RecordedAt *time.Time`, `PartyDeadlineAt *time.Time`; `FeedDelivery.Party bool`; `(*FeedPullRequests).Find(ctx, repository string, number int) (FeedPullRequest, error)` (`ErrNotFound` when missing); `FeedReceivedQuery{TokenID string; Repositories []string; Since time.Time}`; `(*FeedPullRequests).ReceivedSince(ctx, query FeedReceivedQuery) ([]FeedPullRequest, error)`.

- [ ] **Step 1: Write the failing tests.**
  - `TestFeedPullRequests_UpsertKeepsTheFirstRecordedAt`: insert with `RecordedAt` t0, upsert with t1 → `Find` returns t0.
  - `TestFeedPullRequests_UpsertReplacesPartySettings`: second upsert with `PartyReviewers` 3 and a new `PartyDeadlineAt` → both replaced.
  - `TestFeedPullRequests_FindMissingIsErrNotFound`.
  - `TestFeedPullRequests_ReceivedSince`: `t1` has deliveries for #1 and #2 (one solo, one party); #3 not delivered; #2 updated before `since` → returns `[1]`; `t2` gets nothing; empty repositories → empty slice.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.** `Upsert` adds `party_reviewers` and `party_deadline_at` to the updated columns and `recorded_at = COALESCE(feed_pull_requests.recorded_at, excluded.recorded_at)`; times stored UTC. `ReceivedSince` joins `feed_deliveries` on `token_id`, same order as `ChangedSince`.
- [ ] **Step 4: Run, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(persistence): party settings and record time on feed pull requests`.

### Task 6: Atomic party claim in persistence

**Files:**
- Modify: `internal/platform/persistence/models.go`, `feed.go`, `feed_test.go`

**Interfaces:**
- Produces:
  ```go
  type FeedPullRequestKey struct { Repository string; Number int }
  type FeedPartySnapshot struct {
      Waiting    []FeedPullRequest   // open PRs of the repositories without a delivery to the claimant, by updated_at, repository, number
      Deliveries []FeedDelivery      // every delivery row of the waiting PRs
      Members    map[string][]string // repository → other party members that polled at or after ActiveSince
      Loads      []FeedPartyLoad     // rows of Day for the repositories
  }
  type FeedPartyClaim struct {
      TokenID      string
      Repositories []string
      DeliveredAt  time.Time
      Day          string
      ActiveSince  time.Time
      Choose       func(FeedPartySnapshot) []FeedPullRequestKey
  }
  func (r *FeedDeliveries) ClaimParty(ctx context.Context, claim FeedPartyClaim) ([]FeedPullRequest, error)
  ```

- [ ] **Step 1: Write the failing tests** (seed tokens with `seedToken`, subscriptions with `Type`, polls with `FeedPolls.Record`):
  - `TestFeedDeliveries_ClaimPartySnapshotMembers`: repository `acme/api`, `ActiveSince` = base. Members of `acme/api` are exactly `t2` (party, polled at base) and `t6` (`*` party, polled at base). Not members: claimant `t1`; `t3` (party, polled before base); `t4` (party, never polled); `t5` (solo); `t7` (`*` party but its own `acme/api` row is solo).
  - `TestFeedDeliveries_ClaimPartySnapshotWaiting`: a PR the claimant has as a solo delivery is not in `Waiting`; `Deliveries` holds `t2`'s row of another PR with `Party: true`.
  - `TestFeedDeliveries_ClaimPartyRecordsDeliveriesAndLoad`: `Choose` returns one key → returned PR; its delivery row has `party = 1`; `feed_party_loads` row (`t1`, `acme/api`, `Day`) has `taken` 1; a second claim on the same day with another PR → `taken` 2; after `FeedPullRequests.Delete` of both PRs the load row stays (spec call 5).
  - `TestFeedDeliveries_ClaimPartyRemovesEarlierDays`: a `feed_party_loads` row of `2026-11-02` is gone after a claim with `Day` `2026-11-03`; a `2026-11-03` row stays.
  - `TestFeedDeliveries_ClaimPartyWithoutRepositories`: empty result, `Choose` not called.
  - `TestFeedDeliveries_ClaimPartyConcurrentMembersNeverPassTheCap`: 8 party tokens, all polled; one PR with `PartyReviewers` 2; each caller's `Choose` returns the waiting PRs whose party delivery count in the snapshot is below `PartyReviewers`; all 8 start together → no error, exactly 2 `party = 1` rows for the PR.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement** one `Transaction`, statements in this order:
  1. `DELETE FROM feed_party_loads WHERE day < ?` (ruling 2; the first statement writes).
  2. Waiting: `SELECT * FROM feed_pull_requests pr WHERE pr.repository IN ? AND NOT EXISTS (SELECT 1 FROM feed_deliveries d WHERE d.token_id = ? AND d.repository = pr.repository AND d.number = pr.number) ORDER BY updated_at, repository, number`.
  3. Deliveries of the waiting keys.
  4. Members, per repository:
     ```sql
     SELECT s.token_id FROM feed_subscriptions s
     JOIN feed_polls p ON p.token_id = s.token_id AND p.polled_at >= ?
     WHERE s.type = 'party' AND s.token_id <> ?
       AND (s.repository = ? OR (s.repository = '*' AND NOT EXISTS (
            SELECT 1 FROM feed_subscriptions own
            WHERE own.token_id = s.token_id AND own.repository = ?)))
     ```
  5. Loads of `Day` for the repositories.
  6. `Choose(snapshot)`. Insert `feed_deliveries (…, party) VALUES (…, 1)` for each key, then upsert the loads with `ON CONFLICT (token_id, repository, day) DO UPDATE SET taken = taken + excluded.taken`, then read the claimed PRs in the response order.
- [ ] **Step 4: Run, expect PASS** (also with `-count=20` for the race test). Run the task gate.
- [ ] **Step 5: Commit** `feat(persistence): atomic party claim`.

### Task 7: Store ports and feed adapters

**Files:**
- Modify: `internal/feed/domain/interfaces.go`, `models.go`
- Modify: `internal/feed/infrastructure/subscription_repo.go`, `pull_request_repo.go`, `delivery_repo.go`, `repos_test.go`
- Create: `internal/feed/infrastructure/poll_repo.go`, `config_party_settings.go`
- Modify: `internal/feed/application/fakes_test.go` (stub the new methods), `internal/feed/module.go`, `internal/feed/module_test.go`

**Interfaces:**
- Consumes: Tasks 2–6.
- Produces (domain):
  ```go
  type SubscriptionType string // enums.go: SubscriptionTypeSolo = "solo", SubscriptionTypeParty = "party"; func (SubscriptionType) Valid() bool
  // Subscription gains Type SubscriptionType; NewSubscriptions gains Type SubscriptionType; PullRequest gains Party PartyState
  type TypeChange struct { Repositories []string /* nil: every subscription */; Type SubscriptionType }
  type ReceivedQuery struct { TokenID string; Repositories []string; Since time.Time }
  type PartyClaim struct { TokenID string; Repositories []string; DeliveredAt time.Time; Day string; ActiveSince time.Time }
  type PartyClaimResult struct { Delivered []PullRequest; Taken int; Waiting int }
  // SubscriptionStore: SetType(ctx context.Context, tokenID string, change TypeChange) (int, error)
  // PullRequestStore:  Find(ctx context.Context, key PullRequestKey) (PullRequest, bool, error)
  //                    ReceivedSince(ctx context.Context, query ReceivedQuery) ([]PullRequest, error)
  // DeliveryStore:     ClaimParty(ctx context.Context, claim PartyClaim) (PartyClaimResult, error)
  type PollStore interface { Record(ctx context.Context, tokenID string, polledAt time.Time) error }
  type PartySettingsResolver interface { Resolve(ctx context.Context, key PullRequestKey) (PartySettings, error) }
  ```
  Infrastructure: `NewDeliveryRepo(deliveries *persistence.FeedDeliveries, policy domain.TurnPolicy)`; `NewPollRepo(polls *persistence.FeedPolls) *PollRepo`; `NewConfigPartySettings(settings domain.Settings) *ConfigPartySettings` (returns `settings.Party`, never an error).

- [ ] **Step 1: Write the failing tests** in `repos_test.go`:
  - `TestSubscriptionRepo_TypeRoundTrips` and `TestSubscriptionRepo_SetType`.
  - `TestPullRequestRepo_PartyStateRoundTrips`; a NULL `party_deadline_at` maps to a zero `DeadlineAt`.
  - `TestPullRequestRepo_FindMissingIsNotFound` (`found == false`, nil error).
  - `TestPullRequestRepo_ReceivedSince`.
  - `TestDeliveryRepo_ClaimPartyBuildsTheRound`: a fake `TurnPolicy` records its `PartyRound` and takes the first PR. Assert the round: `Candidates` excludes a member that already has a delivery of that PR; `PartyDeliveries` counts only `party` rows; `Loads` has the day's rows; `Now == DeliveredAt`. Assert the result: `Delivered` holds the taken PR; `Taken` and `Waiting` come from the decision.
  - `TestPollRepo_Record`; `TestConfigPartySettings_ReturnsTheGlobalSettings`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement** the ports, the mappings and the adapters. Stub the new store methods in `application/fakes_test.go`. Add `NewPollRepo` (as `domain.PollStore`) and `NewConfigPartySettings` (as `domain.PartySettingsResolver`) to `feed.Module`; supply `*persistence.FeedPolls` in `module_test.go`.
- [ ] **Step 4: Run, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(feed): party store ports and adapters`.

### Task 8: Subscription type on add and `PATCH /api/v1/subs`

**Files:**
- Modify: `internal/feed/domain/interfaces.go`, `models.go`, `errors.go`, `enums.go`, `constants.go`
- Modify: `internal/feed/application/subscriptions.go`, `subscriptions_test.go`
- Modify: `internal/feed/infrastructure/subs_handler.go`, `subs_handler_test.go`, `routes.go`, `routes_test.go`, `http_fakes_test.go`

**Interfaces:**
- Produces: `domain.SubscriptionRequest{Repositories []string; Type SubscriptionType /* empty: solo */}`; `Subscriptions.Add(ctx, subscriber Subscriber, request SubscriptionRequest) ([]Subscription, error)`; `Subscriptions.ChangeType(ctx, subscriber Subscriber, change TypeChange) ([]Subscription, error)`; `ErrInvalidType`; `NotSubscribedError{Repositories []string}`; `ErrorCodeInvalidType = "invalid_type"`, `ErrorCodeNotSubscribed = "not_subscribed"`; `ChangeSubscriptionsPattern = "PATCH /api/v1/subs"`.

- [ ] **Step 1: Write the failing tests.**
  Use case: `TestSubscriptions_AddStoresTheType` (party passed to the store); `TestSubscriptions_AddWithoutTypeIsSolo`; `TestSubscriptions_AddInvalidTypeAddsNothing` (`ErrInvalidType`, no store call); `TestSubscriptions_ChangeType` (table: every subscription; named `"Acme/API"` matches stored `acme/api`; `"*"`; not subscribed `acme/web` → `NotSubscribedError{["acme/web"]}` and no store call; empty type → `ErrInvalidType`; `[]` → `ErrNoRepositories`); `TestSubscriptions_ChangeTypeLogs` (`feed subscriptions changed` with `user`, `token_id`, `type`, `count`).
  Handler: `TestSubs_ListShowsType` (`"type":"solo"`); `TestSubs_AddPassesType`; `TestSubs_Add_InvalidTypeIs400`; `TestSubs_Change` (200 + full list); `TestSubs_Change_BadRequests` (table: missing type → `invalid_type`; `"type":"team"` → `invalid_type`; unknown field → `invalid_body`; `"repositories":[]` → `invalid_body`; trailing data → `invalid_body`; not subscribed → `400 {"error":"not_subscribed","repositories":["acme/web"]}`); `TestRoutes` covers `PATCH /api/v1/subs`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.** `ChangeType` normalizes with `normalizeRepositories`, validates, checks against `store.List`, then calls `store.SetType` and returns `List`. The handler decodes `{"repositories": *[]string, "type": string}` with `DisallowUnknownFields` and the trailing-data check (generalize `decodeAddSubscriptions` into one helper for both bodies). Rename `unknownRepositoryResponse` to a shared `repositoriesErrorResponse`. `subscriptionResponse` gains `Type string \`json:"type"\``.
- [ ] **Step 4: Run, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(feed): subscription type and PATCH /api/v1/subs`.

### Task 9: Party delivery, days off and the `since` rule

**Files:**
- Modify: `internal/feed/domain/models.go` (`PullRequestsParams` gains `Polls PollStore`, `Calendar WorkCalendar`), `constants.go` (`AwayAfter = 24 * time.Hour`)
- Modify: `internal/feed/application/pull_requests.go`, `pull_requests_test.go`, `fakes_test.go`
- Modify: `internal/feed/module.go`
- Modify: `internal/runtime/integration_feed_test.go` (pin the clock in every test that polls)

**Interfaces:**
- Consumes: Task 3 `WorkCalendar`, Task 7 ports.
- Produces: no new exported names.

- [ ] **Step 1: Write the failing tests** (fake calendar: `IsDayOff` false, `Day` `"2026-11-03"`; `Now` = Tue 2026-11-03 10:00 UTC):
  - `TestPullRequests_NextOnADayOff`: empty list, neither claim called, poll recorded at `Now`, debug log `feed day off` with `token_id`.
  - `TestPullRequests_NextSplitsSoloAndParty`: subscriptions `acme/api` solo, `acme/web` party, `*` party; stored repositories `acme/api`, `acme/web`, `acme/lib` → `Claim.Repositories` = `[acme/api]`; `PartyClaim.Repositories` matches `[acme/web, acme/lib]`; `PartyClaim` has `TokenID`, `DeliveredAt` = `Now`, `Day` = `"2026-11-03"`, `ActiveSince` = `Now - 24h`.
  - `TestPullRequests_NextOwnSubscriptionOverridesAll`: `*` party plus `acme/api` solo → `acme/api` is solo.
  - `TestPullRequests_NextMergesInDeliveryOrder` (by `UpdatedAt`, then repository, then number).
  - `TestPullRequests_NextLogs`: `feed pull requests delivered` with `count` 3 and `party` 1; debug `feed party pull requests held` with `token_id`, `taken` 1, `waiting` 2; no held line when both are 0.
  - `TestPullRequests_NextRecordsThePollAfterClaiming`; a `ClaimParty` error → error returned, no poll recorded.
  - `TestPullRequests_SinceOnPartyRepositories`: solo repositories → `ChangedSince`; party → `ReceivedSince` with the `TokenID`; result merged in delivery order. It works on a day off. It never records a poll (spec call 6).
  - Update the existing `Next`/`Since` tests to the new fixture.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.** `resolveRepositories` returns the solo and the party lists (the own subscription's type wins over the `*` type; `catalog.Configured` filters both). Skip a claim whose list is empty. `providePullRequests` passes `Polls` and `Calendar`. In `integration_feed_test.go` add `type movingClock struct{ start, started time.Time }` (`Now` = `start.Add(time.Since(started)).UTC()`) and `weekdayClock()` (starts at Tue 2026-11-03 10:00 UTC, so a run never crosses into a weekend). Add `buildFeedApp(t, cfg, clock feeddomain.Clock, options ...fx.Option)` = `buildAgentAPIApp` plus `fx.Decorate(func(feeddomain.Clock) feeddomain.Clock { return clock })`. Use it in every test that calls `listFeed`. Build `since` values from the same clock.
- [ ] **Step 4: Run, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(feed): party delivery, days off and the since rule`.

### Task 10: Recorder stores the party state

**Files:**
- Modify: `internal/feed/domain/models.go` (`RecorderParams{PullRequests PullRequestStore; Catalog RepositoryCatalog; Clock Clock; Calendar WorkCalendar; PartySettings PartySettingsResolver}`)
- Modify: `internal/feed/application/recorder.go`, `recorder_test.go`, `fakes_test.go`
- Modify: `internal/feed/module.go`

**Interfaces:**
- Produces: `application.NewRecorder(params domain.RecorderParams) *Recorder`.

- [ ] **Step 1: Write the failing tests:**
  - `TestRecorder_StoresThePartyState`: settings `{Reviewers: 2, Deadline: 30m}`; the fake calendar returns `deadline` for `AddWorkingTime(Now, 30m)` → stored `Party == {2, Now, deadline}`.
  - `TestRecorder_KeepsTheFirstRecordTime`: the stored PR has `RecordedAt` t0 → stored `RecordedAt` t0; `AddWorkingTime` called with t0.
  - `TestRecorder_StoredRowWithoutRecordTimeStartsNow`.
  - `TestRecorder_ReturnsFindError`; `TestRecorder_ReturnsResolverError`.
  - A close event does not call `Find` or `Resolve`.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.** On `opened`, `ready_for_review`, `updated`, `reopened`: `Find` → `recordedAt` (stored non-zero value, else `Now`) → `Resolve` → `Party` → `Upsert`. Replace the `NewRecorder` registration in `feed.Module` with an `fx.In` provider.
- [ ] **Step 4: Run, expect PASS.** Run the task gate.
- [ ] **Step 5: Commit** `feat(feed): record the party cap and timer`.

### Task 11: Integration flows

**Files:**
- Modify: `internal/runtime/integration_feed_test.go`

- [ ] **Step 1: Write the tests** (weekday clock unless stated; `alice`, `bob` and `carol` tokens via `redeemAgentToken`):
  - `TestIntegration_ReviewPartyFlow`: `alice` and `bob` subscribe `acme/api` with `"type":"party"`, `carol` without a type. All three poll once. PR 42 opens. `alice` polls → gets 42. `bob` polls → empty. `carol` polls → gets 42. PR 43 opens. `alice` polls → empty (her load is 1, `bob`'s is 0). `bob` polls → gets 43. `alice` `?since=` → only 42. `PATCH /api/v1/subs` `{"type":"solo"}` for `alice` → 200 with `"type":"solo"`. `alice` polls → gets 43. The log has `feed pull requests delivered` with `party=1`.
  - `TestIntegration_FeedDayOff`: a `movingClock` that starts at Sat 2026-11-07 10:00 UTC. A solo token subscribes, PR 42 opens, a normal poll → empty; `?since=` → 42; the log has `feed day off`.
- [ ] **Step 2: Run, expect PASS** (the behavior exists; these are end-to-end checks of Tasks 1–10). If one fails, fix the cause in the owning code, not in the test.
- [ ] **Step 3: Run the task gate.**
- [ ] **Step 4: Commit** `test(feed): review party and day-off integration flows`.

### Task 12: Docs

**Files:**
- Modify: `docs/agent-api.md`, `docs/configuration.md`, `docs/digest.md`, `docs/operations.md`, `ARCHITECTURE.md`, `CLAUDE.md`

- [ ] **Step 1: Write.**
  - `docs/agent-api.md`: new section "Review as a party" (solo and party, cap, turns, daily load, away, the timer, days off, the example from the spec). `type` in `GET`/`POST /api/v1/subs`. `PATCH /api/v1/subs` with both body forms and the errors `invalid_type`, `not_subscribed`, `invalid_body`. The `since` rule on party repositories. New error codes in "Errors". Days off in "Delivery rules".
  - `docs/configuration.md`: `agent_api.party_reviewers` and `agent_api.party_deadline` with defaults, ranges and format.
  - `docs/digest.md`: the calendar also decides the feed's days off and the timer.
  - `docs/operations.md`: `feed day off`, `feed party pull requests held`, `feed subscriptions changed`, and the `party` field of `feed pull requests delivered`.
  - `ARCHITECTURE.md`: the `feed` row says that party members share a repository's PRs; the domain graph gets `feed --> digest`.
  - `CLAUDE.md`: the `feed` row of the domain table names parties; the persistence paragraph names `feed_party_loads` and `feed_polls`.
- [ ] **Step 2: Check.** Apply the `technical-language` skill (`~/.claude/skills/technical-language/SKILL.md`) and run `python3 ~/.claude/skills/technical-language/techlang-lint.py <each changed doc>`; the score must not rise above the file's score on `main`. Then: `docker run --rm -v "$PWD":/docs squidfunk/mkdocs-material:9.5.39 build --strict --site-dir /tmp/site && rm -rf .cache` → exit 0.
- [ ] **Step 3: Commit** `docs(feed): review parties`.

---

## Before the PR

- Run `just vuln` → no vulnerabilities.
- Code review and QA rounds until nothing relevant is left. Then open the PR `feat(feed): review parties for shared repositories` and wait for Pavlo to merge.
