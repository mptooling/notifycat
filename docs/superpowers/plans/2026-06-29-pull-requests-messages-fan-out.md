# Per-PR Multi-Message Fan-out Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single `slack_messages` table with a one-to-many `pull_requests` → `messages` model so a PR fans out to a separate, independently-tracked message in every matched path channel.

**Architecture:** A new `store.PullRequests` repository over two tables lands first, unused, alongside the old `slack_messages`. Then a single cutover switches routing to multi-target fan-out and rewires every handler, the digest, cleanup, and reconcile onto the new repository, dropping the old table. Routing splits into per-repo *behavior* (`mappings.Get`) and per-channel *targets* (`mappings.TargetsForFiles`); only the open handler resolves targets, and later handlers read the stored `messages` rows.

**Tech Stack:** Go 1.25.10, GORM over SQLite (`glebarez/sqlite`), goose SQL migrations (embedded), `slog`. Tests via `go test -race`.

**Spec:** `docs/superpowers/specs/2026-06-29-pull-requests-messages-fan-out-design.md`

## Global Constraints

- Go toolchain pinned at **1.25.10**; no new module dependencies.
- Run verification with the retired reaction env vars unset (matches CI): prefix test/vet/lint commands with `env -u NOTIFYCAT_REACTION_NEW_PR -u NOTIFYCAT_REACTION_MERGED_PR -u NOTIFYCAT_REACTION_CLOSED_PR -u NOTIFYCAT_REACTION_APPROVED -u NOTIFYCAT_REACTION_COMMENTED -u NOTIFYCAT_REACTION_REQUEST_CHANGE`.
- **Consumer-package interfaces**: interfaces live where consumed, not where implemented.
- **Readable names over terse Go idiom** (per `CLAUDE.md`): descriptive identifiers; short names only for trivial scopes.
- **No comments restating code**; comment only non-obvious *why*.
- **TDD**: failing test → verify fail → minimal code → verify pass → commit. Frequent commits.
- **No attribution footers** in commits/PRs. **No hard-wrapped markdown** in docs/PR bodies.
- Conventional Commits; PR title = commit subject. Pre-1.0 feature → minor bump.
- SQLite `PRAGMA foreign_keys = ON` is already set in `internal/store/db.go:enableForeignKeys`, so `ON DELETE CASCADE` is active.

## File Structure

**PR 1 — foundation (green, new code unused by runtime):**
- Create `internal/store/models.go` additions: `PullRequest`, `Message`, `Target` value object.
- Create `internal/store/migrations/00005_pull_requests_messages.sql` — new tables (keep `slack_messages`).
- Create `internal/store/pull_requests.go` — the `PullRequests` repository.
- Create `internal/store/pull_requests_test.go`.
- Modify `internal/pullrequest/deps.go` — rename `SlackClient` → `Messenger`, `ts` → `messageID`.

**PR 2 — cutover (coupled; one PR):**
- Modify `internal/mappings/paths_resolve.go` — add `TargetsForFiles`; remove single-winner `GetForFiles`.
- Modify `internal/pullrequest/router.go` — `ResolveTargets`; drop single-target `Resolve`.
- Modify `internal/pullrequest/{open,close,draft,review_handlers}.go` — fan-out + read stored messages.
- Modify `internal/pullrequest/deps.go` — new store + behavior interfaces.
- Modify `internal/app/app.go` — wire `store.PullRequests`.
- Modify `internal/digest/reporter.go` — group by stored `message.channel`.
- Modify `internal/cleanup/cleanup.go`, `internal/reconcile/reconcile.go` — PR-level.
- Create `internal/store/migrations/00006_drop_slack_messages.sql`.
- Delete `internal/store/slack_messages.go` + `SlackMessage` model.
- Modify docs: `docs/mappings.md`, `docs/operations.md`, `docs/upgrading.md`.

---

# PR 1 — Store foundation

Each task here ends green; the new code is not yet wired into the runtime.

### Task 1: New store models

**Files:**
- Modify: `internal/store/models.go`
- Test: `internal/store/models_test.go`

**Interfaces:**
- Produces: `store.PullRequest{ ID uint; Repository string; PRNumber int; CreatedAt, UpdatedAt time.Time; ClosedAt *time.Time; Messages []Message }`; `store.Message{ ID uint; PullRequestID uint; Channel string; MessageID string }`; `store.Target{ Channel string; Mentions []string }`.

- [ ] **Step 1: Write the failing test** for table names and nullable `ClosedAt`.

```go
// internal/store/models_test.go (add)
func TestPullRequestTableName(t *testing.T) {
	if (store.PullRequest{}).TableName() != "pull_requests" {
		t.Errorf("PullRequest.TableName = %q; want pull_requests", (store.PullRequest{}).TableName())
	}
	if (store.Message{}).TableName() != "messages" {
		t.Errorf("Message.TableName = %q; want messages", (store.Message{}).TableName())
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/store/ -run TestPullRequestTableName`
Expected: FAIL — `PullRequest` undefined.

- [ ] **Step 3: Add the models**

```go
// internal/store/models.go (add)

// PullRequest is one tracked PR. (Repository, PRNumber) is the natural key;
// CreatedAt is kept for later statistics, UpdatedAt is the activity clock
// (bumped on open and every review/comment) driving digest idle-detection and
// cleanup, and ClosedAt (nil = open) marks merged/closed so the digest skips it.
type PullRequest struct {
	ID         uint       `gorm:"primaryKey"`
	Repository string     `gorm:"column:gh_repository;not null"`
	PRNumber   int        `gorm:"column:pr_number;not null"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;not null"`
	ClosedAt   *time.Time `gorm:"column:closed_at"`
	Messages   []Message  `gorm:"foreignKey:PullRequestID;constraint:OnDelete:CASCADE"`
}

// TableName pins the table name; do not rely on GORM pluralization.
func (PullRequest) TableName() string { return "pull_requests" }

// Message is one posted messenger message for a PR. (PullRequestID, Channel) is
// unique — at most one message per channel per PR. Channel is a room in the
// messenger; MessageID is the messenger's id for the post (Slack's ts).
type Message struct {
	ID            uint   `gorm:"primaryKey"`
	PullRequestID uint   `gorm:"column:pull_request_id;not null"`
	Channel       string `gorm:"column:channel;not null"`
	MessageID     string `gorm:"column:message_id;not null"`
}

// TableName pins the table name.
func (Message) TableName() string { return "messages" }

// Target is one fan-out destination resolved for a PR: a channel and the
// mentions to ping there. Produced by the mappings resolver, consumed by the
// open handler.
type Target struct {
	Channel  string
	Mentions []string
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/store/ -run TestPullRequestTableName`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/models.go internal/store/models_test.go
git commit -m "feat: add pull_requests, messages, and target store models"
```

### Task 2: Migration creating the new tables

**Files:**
- Create: `internal/store/migrations/00005_pull_requests_messages.sql`
- Test: `internal/store/store_test.go` (add a migration smoke test)

**Interfaces:**
- Produces: tables `pull_requests` and `messages` with the unique indexes; `slack_messages` left intact.

- [ ] **Step 1: Write the failing test** that the new tables exist after `MigrateUp`.

```go
// internal/store/store_test.go (add)
func TestMigrate_CreatesPullRequestsAndMessages(t *testing.T) {
	db := newTestDB(t) // existing helper that opens + MigrateUp
	for _, table := range []string{"pull_requests", "messages"} {
		var name string
		err := db.Raw(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name).Error
		if err != nil || name != table {
			t.Fatalf("table %q missing after migrate (got %q, err %v)", table, name, err)
		}
	}
}
```

(If `newTestDB` is not the existing helper name, use the helper already in `store_test.go`/`testing.go` that returns a migrated `*gorm.DB`.)

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/store/ -run TestMigrate_CreatesPullRequestsAndMessages`
Expected: FAIL — tables missing.

- [ ] **Step 3: Write the migration**

```sql
-- internal/store/migrations/00005_pull_requests_messages.sql
-- +goose Up
CREATE TABLE pull_requests (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    gh_repository TEXT     NOT NULL,
    pr_number     INTEGER  NOT NULL,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL,
    closed_at     DATETIME
);
CREATE UNIQUE INDEX idx_pull_requests_repo_number ON pull_requests(gh_repository, pr_number);
CREATE INDEX idx_pull_requests_updated_at ON pull_requests(updated_at);
CREATE INDEX idx_pull_requests_closed_at ON pull_requests(closed_at);

CREATE TABLE messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pull_request_id INTEGER NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    channel         TEXT    NOT NULL,
    message_id      TEXT    NOT NULL
);
CREATE UNIQUE INDEX idx_messages_pr_channel ON messages(pull_request_id, channel);

-- +goose Down
DROP TABLE messages;
DROP TABLE pull_requests;
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/store/ -run TestMigrate_CreatesPullRequestsAndMessages`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrations/00005_pull_requests_messages.sql internal/store/store_test.go
git commit -m "feat: migration for pull_requests and messages tables"
```

### Task 3: PullRequests repository

**Files:**
- Create: `internal/store/pull_requests.go`
- Test: `internal/store/pull_requests_test.go`

**Interfaces:**
- Consumes: `store.PullRequest`, `store.Message` (Task 1); a migrated `*gorm.DB`.
- Produces:
  - `NewPullRequests(db *gorm.DB) *PullRequests`
  - `(*PullRequests).AddMessage(ctx, repository string, prNumber int, channel, messageID string) error` — find-or-create the PR (sets `created_at`/`updated_at` on first insert), then insert the message; idempotent on `(pull_request_id, channel)`.
  - `(*PullRequests).Messages(ctx, repository string, prNumber int) ([]Message, error)` — `ErrNotFound` when the PR is unknown.
  - `(*PullRequests).Touch(ctx, repository string, prNumber int) error`
  - `(*PullRequests).MarkClosed(ctx, repository string, prNumber int) error`
  - `(*PullRequests).Delete(ctx, repository string, prNumber int) error`
  - `(*PullRequests).FindStuck(ctx, cutoff time.Time) ([]PullRequest, error)` — open PRs with `updated_at < cutoff`, `Messages` preloaded, oldest first.
  - `(*PullRequests).ListOpen(ctx) ([]PullRequest, error)` — `closed_at IS NULL`, ordered.
  - `(*PullRequests).DeleteStaleBefore(ctx, cutoff time.Time) (int64, error)` — delete PRs with `updated_at < cutoff` (messages cascade); returns count.

- [ ] **Step 1: Write the failing test** covering AddMessage idempotency + Messages + cascade delete.

```go
// internal/store/pull_requests_test.go
package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/store"
)

func TestPullRequests_AddMessageIsIdempotentPerChannel(t *testing.T) {
	repo := store.NewPullRequests(newTestDB(t))
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := repo.AddMessage(ctx, "acme/web", 7, "C0A", "100.1"); err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
	}
	if err := repo.AddMessage(ctx, "acme/web", 7, "C0B", "200.1"); err != nil {
		t.Fatalf("AddMessage second channel: %v", err)
	}
	msgs, err := repo.Messages(ctx, "acme/web", 7)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages; want 2 (one per channel, deduped)", len(msgs))
	}
}

func TestPullRequests_MessagesNotFound(t *testing.T) {
	repo := store.NewPullRequests(newTestDB(t))
	if _, err := repo.Messages(context.Background(), "acme/web", 999); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound; got %v", err)
	}
}

func TestPullRequests_DeleteCascadesMessages(t *testing.T) {
	db := newTestDB(t)
	repo := store.NewPullRequests(db)
	ctx := context.Background()
	_ = repo.AddMessage(ctx, "acme/web", 7, "C0A", "100.1")
	if err := repo.Delete(ctx, "acme/web", 7); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var count int64
	db.Raw("SELECT count(*) FROM messages").Scan(&count)
	if count != 0 {
		t.Fatalf("messages not cascade-deleted; count=%d", count)
	}
}

func TestPullRequests_FindStuckPreloadsMessages(t *testing.T) {
	repo := store.NewPullRequests(newTestDB(t))
	ctx := context.Background()
	_ = repo.AddMessage(ctx, "acme/web", 7, "C0A", "100.1")
	// Age it past the cutoff.
	_ = repo.Touch(ctx, "acme/web", 7)
	stuck, err := repo.FindStuck(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("FindStuck: %v", err)
	}
	if len(stuck) != 1 || len(stuck[0].Messages) != 1 || stuck[0].Messages[0].Channel != "C0A" {
		t.Fatalf("FindStuck = %+v; want one PR with its message preloaded", stuck)
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/store/ -run TestPullRequests`
Expected: FAIL — `NewPullRequests` undefined.

- [ ] **Step 3: Implement the repository**

```go
// internal/store/pull_requests.go
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PullRequests persists tracked PRs and their per-channel messenger messages.
type PullRequests struct {
	db *gorm.DB
}

// NewPullRequests constructs a PullRequests repository bound to db.
func NewPullRequests(db *gorm.DB) *PullRequests {
	return &PullRequests{db: db}
}

// AddMessage records one posted message, creating the PR row on first sight.
// Insertion is idempotent on (pull_request_id, channel): re-adding the same
// channel for the same PR is a no-op, which makes the open fan-out safe to
// replay after a partial failure or GitHub redelivery.
func (r *PullRequests) AddMessage(ctx context.Context, repository string, prNumber int, channel, messageID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		pr := PullRequest{Repository: repository, PRNumber: prNumber, CreatedAt: now, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "gh_repository"}, {Name: "pr_number"}},
			DoNothing: true,
		}).Create(&pr).Error; err != nil {
			return fmt.Errorf("store: ensure pull request: %w", err)
		}
		if pr.ID == 0 { // conflict path: load the existing row's id
			if err := tx.Where("gh_repository = ? AND pr_number = ?", repository, prNumber).
				First(&pr).Error; err != nil {
				return fmt.Errorf("store: load pull request: %w", err)
			}
		}
		msg := Message{PullRequestID: pr.ID, Channel: channel, MessageID: messageID}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "pull_request_id"}, {Name: "channel"}},
			DoNothing: true,
		}).Create(&msg).Error; err != nil {
			return fmt.Errorf("store: add message: %w", err)
		}
		return nil
	})
}

// Messages returns the PR's messages, or ErrNotFound when the PR is unknown.
func (r *PullRequests) Messages(ctx context.Context, repository string, prNumber int) ([]Message, error) {
	var pr PullRequest
	err := r.db.WithContext(ctx).Preload("Messages").
		Where("gh_repository = ? AND pr_number = ?", repository, prNumber).
		First(&pr).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get messages: %w", err)
	}
	return pr.Messages, nil
}

// Touch bumps updated_at, recording activity. Missing PR is a no-op.
func (r *PullRequests) Touch(ctx context.Context, repository string, prNumber int) error {
	res := r.db.WithContext(ctx).Model(&PullRequest{}).
		Where("gh_repository = ? AND pr_number = ?", repository, prNumber).
		UpdateColumn("updated_at", time.Now())
	if res.Error != nil {
		return fmt.Errorf("store: touch pull request: %w", res.Error)
	}
	return nil
}

// MarkClosed sets closed_at. Missing PR is a no-op.
func (r *PullRequests) MarkClosed(ctx context.Context, repository string, prNumber int) error {
	res := r.db.WithContext(ctx).Model(&PullRequest{}).
		Where("gh_repository = ? AND pr_number = ?", repository, prNumber).
		UpdateColumn("closed_at", time.Now())
	if res.Error != nil {
		return fmt.Errorf("store: mark pull request closed: %w", res.Error)
	}
	return nil
}

// Delete removes the PR and (by cascade) its messages. Missing PR is a no-op.
func (r *PullRequests) Delete(ctx context.Context, repository string, prNumber int) error {
	err := r.db.WithContext(ctx).
		Where("gh_repository = ? AND pr_number = ?", repository, prNumber).
		Delete(&PullRequest{}).Error
	if err != nil {
		return fmt.Errorf("store: delete pull request: %w", err)
	}
	return nil
}

// FindStuck returns open PRs idle since before cutoff, messages preloaded,
// oldest first.
func (r *PullRequests) FindStuck(ctx context.Context, cutoff time.Time) ([]PullRequest, error) {
	var rows []PullRequest
	err := r.db.WithContext(ctx).Preload("Messages").
		Where("closed_at IS NULL AND updated_at < ?", cutoff).
		Order("updated_at asc").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: find stuck pull requests: %w", err)
	}
	return rows, nil
}

// ListOpen returns every not-yet-closed PR, ordered for stable output.
func (r *PullRequests) ListOpen(ctx context.Context) ([]PullRequest, error) {
	var rows []PullRequest
	err := r.db.WithContext(ctx).
		Where("closed_at IS NULL").
		Order("gh_repository asc, pr_number asc").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: list open pull requests: %w", err)
	}
	return rows, nil
}

// DeleteStaleBefore removes PRs idle since before cutoff (messages cascade).
func (r *PullRequests) DeleteStaleBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res := r.db.WithContext(ctx).
		Where("updated_at < ?", cutoff).
		Delete(&PullRequest{})
	if res.Error != nil {
		return 0, fmt.Errorf("store: delete stale pull requests: %w", res.Error)
	}
	return res.RowsAffected, nil
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `env -u NOTIFYCAT_REACTION_NEW_PR -u NOTIFYCAT_REACTION_MERGED_PR -u NOTIFYCAT_REACTION_CLOSED_PR -u NOTIFYCAT_REACTION_APPROVED -u NOTIFYCAT_REACTION_COMMENTED -u NOTIFYCAT_REACTION_REQUEST_CHANGE go test ./internal/store/ -run TestPullRequests`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/pull_requests.go internal/store/pull_requests_test.go
git commit -m "feat: PullRequests repository over pull_requests and messages"
```

### Task 4: Rename `SlackClient` interface to `Messenger`

Mechanical vocabulary change; the concrete `slack.Client` already satisfies it.

**Files:**
- Modify: `internal/pullrequest/deps.go`
- Modify: `internal/pullrequest/{open,close,draft,review_handlers}.go` (field type only)
- Modify: `internal/pullrequest/fakes_test.go` (the fake's doc comment only — methods unchanged)

**Interfaces:**
- Produces: `pullrequest.Messenger` with `PostMessage(ctx, channel, msg) (messageID string, err error)`, `UpdateMessage(ctx, channel, messageID string, msg)`, `DeleteMessage(ctx, channel, messageID string)`, `AddReaction(ctx, channel, messageID, name string)`.

- [ ] **Step 1: Rename the interface and its parameters**

```go
// internal/pullrequest/deps.go (replace the SlackClient block)

// Messenger is the subset of a chat messenger the handlers use. Slack is the
// only implementation today (slack.Client satisfies it).
type Messenger interface {
	PostMessage(ctx context.Context, channel string, msg slack.Message) (messageID string, err error)
	UpdateMessage(ctx context.Context, channel, messageID string, msg slack.Message) error
	DeleteMessage(ctx context.Context, channel, messageID string) error
	AddReaction(ctx context.Context, channel, messageID, name string) error
}
```

- [ ] **Step 2: Update each handler's field type** — change `slack SlackClient` to `slack Messenger` in `open.go`, `close.go`, `draft.go`, `review_handlers.go` (field name `slack` stays; only the type changes). Constructor params `slackClient SlackClient` → `slackClient Messenger`.

- [ ] **Step 3: Build and test**

Run: `env -u NOTIFYCAT_REACTION_NEW_PR -u NOTIFYCAT_REACTION_MERGED_PR -u NOTIFYCAT_REACTION_CLOSED_PR -u NOTIFYCAT_REACTION_APPROVED -u NOTIFYCAT_REACTION_COMMENTED -u NOTIFYCAT_REACTION_REQUEST_CHANGE go test ./internal/pullrequest/`
Expected: PASS (the `slack.Client` and the test fake already match the signatures).

- [ ] **Step 4: Commit**

```bash
git add internal/pullrequest/
git commit -m "refactor: rename SlackClient handler interface to Messenger"
```

### Task 5: Open PR 1

- [ ] Run full verification: `env -u ... go vet ./... && env -u ... golangci-lint run ./... && env -u ... go test -race ./... && go build ./...` (full env prefix as in Global Constraints).
- [ ] `git push -u origin <branch>`; open PR titled `feat: pull_requests and messages store foundation`, body filled from the template, label `enhancement`, assignee `@me`. Note in the body that the new repository is not yet wired into the runtime (cutover follows in the next PR).

---

# PR 2 — Cutover to fan-out

This PR is a single coupled change: switching the store interface breaks all consumers at once, so resolution, every handler, the digest, cleanup, and reconcile move together, and the old table is dropped. Keep it green at each task by compiling the whole module after every step.

### Task 6: Multi-target resolution in mappings

**Files:**
- Modify: `internal/mappings/paths_resolve.go`
- Test: `internal/mappings/paths_resolve_test.go`

**Interfaces:**
- Consumes: `store.Target` (Task 1); existing `PathRule`, `Resolved`, `resolveRouting`, `matchedRules`, `unionMentions` helpers.
- Produces: `(*Provider).TargetsForFiles(repository string, files []string) []store.Target` — one target per distinct matched channel (mentions unioned within the channel, inheriting base when a rule sets none), or a single base target when nothing matches / no paths.
- Removes: `(*Provider).GetForFiles`, `resolvePaths`, `channelWinner`, `pathOutcome`, `maxMatchedPathOwners` (single-winner machinery).

- [ ] **Step 1: Write the failing tests**

```go
// internal/mappings/paths_resolve_test.go (replace the GetForFiles tests with these)
func targetsDoc(t *testing.T, body string) *mappings.Provider {
	t.Helper()
	f, err := mappings.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return mappings.NewProvider(mappings.Defaults{}, f.Mappings, nil)
}

func TestTargetsForFiles_FanOutPerChannel(t *testing.T) {
	p := targetsDoc(t, monorepoDoc) // existing const from this file
	got := p.TargetsForFiles("acme/the-monorepo", []string{"modules/acme/x.go", "src/AuthBundle/y.go"})
	// modules/acme inherits base channel C0BASE00000; src/AuthBundle has its own.
	want := map[string][]string{
		"C0BASE00000": {"<@U0A>"},
		"C0AUTH00000": {"<@U0AUTH>"},
	}
	if len(got) != 2 {
		t.Fatalf("got %d targets; want 2: %+v", len(got), got)
	}
	for _, tg := range got {
		if !slices.Equal(tg.Mentions, want[tg.Channel]) {
			t.Errorf("channel %s mentions = %v; want %v", tg.Channel, tg.Mentions, want[tg.Channel])
		}
	}
}

func TestTargetsForFiles_MentionsUnionWithinChannel(t *testing.T) {
	p := targetsDoc(t, monorepoDoc)
	// modules/acme (@U0A) + config (@U0A,@U0B) both inherit the base channel.
	got := p.TargetsForFiles("acme/the-monorepo", []string{"modules/acme/x.go", "config/app.yaml"})
	if len(got) != 1 || got[0].Channel != "C0BASE00000" {
		t.Fatalf("want one base-channel target; got %+v", got)
	}
	if !slices.Equal(got[0].Mentions, []string{"<@U0A>", "<@U0B>"}) {
		t.Errorf("mentions = %v; want deduped union [<@U0A> <@U0B>]", got[0].Mentions)
	}
}

func TestTargetsForFiles_NoMatchReturnsBase(t *testing.T) {
	p := targetsDoc(t, monorepoDoc)
	got := p.TargetsForFiles("acme/the-monorepo", []string{"README.md"})
	if len(got) != 1 || got[0].Channel != "C0BASE00000" ||
		!slices.Equal(got[0].Mentions, []string{"<!subteam^S0ENG>"}) {
		t.Fatalf("no match should yield single base target; got %+v", got)
	}
}
```

(Reuse the existing `monorepoDoc` const already defined in this test file. Delete the obsolete `TestGetForFiles_*`, `TestRouter`-unrelated single-winner cases, and the `maxMatchedPathOwners` test.)

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/mappings/ -run TestTargetsForFiles`
Expected: FAIL — `TargetsForFiles` undefined.

- [ ] **Step 3: Implement `TargetsForFiles`, delete single-winner code**

```go
// internal/mappings/paths_resolve.go

// TargetsForFiles returns the fan-out destinations for a PR touching files: one
// Target per distinct matched channel, mentions unioned within each channel.
// With no path rules, no files, or no match it returns a single base target.
func (p *Provider) TargetsForFiles(repository string, files []string) []store.Target {
	starPtr, repoPtr := p.lookup(repository)
	base := resolveRouting(starPtr, repoPtr)
	baseTarget := []store.Target{{Channel: base.Channel, Mentions: base.Mentions}}
	if repoPtr == nil || len(repoPtr.Paths) == 0 {
		return baseTarget
	}
	winners := matchedRules(repoPtr.Paths, files)
	if len(winners) == 0 {
		return baseTarget
	}

	// Group matched rules by resolved channel, preserving first-seen order, and
	// union each channel's mentions (a rule with no mentions inherits base).
	order := []string{}
	byChannel := map[string][]PathRule{}
	for _, rule := range winners {
		channel := rule.Channel
		if channel == "" {
			channel = base.Channel
		}
		if _, seen := byChannel[channel]; !seen {
			order = append(order, channel)
		}
		byChannel[channel] = append(byChannel[channel], rule)
	}

	targets := make([]store.Target, 0, len(order))
	for _, channel := range order {
		targets = append(targets, store.Target{
			Channel:  channel,
			Mentions: unionMentions(byChannel[channel], base.Mentions),
		})
	}
	return targets
}
```

Then delete `GetForFiles`, `resolvePaths`, `pathOutcome`, `channelWinner`, `segments` (if now unused), and the `maxMatchedPathOwners` const. Keep `matchedRules`, `fileUnder`, `unionMentions`, `HasPathRules`, `RepoHasPathRules`, `PathChannels`. Add the `store` import if not present (it is).

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/mappings/ -run TestTargetsForFiles`
Expected: PASS. Then `go build ./internal/mappings/` to confirm no dangling references to deleted symbols.

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/paths_resolve.go internal/mappings/paths_resolve_test.go
git commit -m "feat: multi-target path resolution (TargetsForFiles)"
```

### Task 7: Router resolves targets

**Files:**
- Modify: `internal/pullrequest/router.go`
- Test: `internal/pullrequest/router_test.go`

**Interfaces:**
- Consumes: `store.RepoMapping`, `store.Target`; `ChangedFiles` (existing).
- Produces:
  - `pullrequest.PathMappings` interface: `Get(ctx, repository) (store.RepoMapping, error)`, `RepoHasPathRules(repository) bool`, `TargetsForFiles(repository string, files []string) []store.Target`.
  - `(*Router).ResolveTargets(ctx, repository string, prNumber int) (behavior store.RepoMapping, targets []store.Target, err error)`.
- Removes: `Resolver` interface, `(*Router).Resolve`, `GetForFiles` from `PathMappings`.

- [ ] **Step 1: Write the failing test**

```go
// internal/pullrequest/router_test.go (replace stub + tests)
type stubMappings struct {
	base         store.RepoMapping
	baseErr      error
	targets      []store.Target
	hasPathRules bool
}

func (s *stubMappings) Get(_ context.Context, repository string) (store.RepoMapping, error) {
	if s.baseErr != nil {
		return store.RepoMapping{}, s.baseErr
	}
	m := s.base
	m.Repository = repository
	return m, nil
}
func (s *stubMappings) RepoHasPathRules(string) bool { return s.hasPathRules }
func (s *stubMappings) TargetsForFiles(string, []string) []store.Target { return s.targets }

func TestRouter_NoFetcherReturnsBaseTarget(t *testing.T) {
	m := &stubMappings{base: store.RepoMapping{SlackChannel: "C0BASE", Mentions: []string{"<!here>"}}, hasPathRules: true}
	r := pullrequest.NewRouter(m, nil, discardLogger())
	_, targets, err := r.ResolveTargets(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(targets) != 1 || targets[0].Channel != "C0BASE" {
		t.Fatalf("want single base target; got %+v", targets)
	}
}

func TestRouter_FanOutTargets(t *testing.T) {
	m := &stubMappings{
		base:         store.RepoMapping{SlackChannel: "C0BASE"},
		hasPathRules: true,
		targets:      []store.Target{{Channel: "C0A"}, {Channel: "C0B"}},
	}
	files := &stubFiles{files: []string{"a", "b"}}
	r := pullrequest.NewRouter(m, files, discardLogger())
	_, targets, err := r.ResolveTargets(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(targets) != 2 || files.calls != 1 {
		t.Fatalf("want 2 targets from one fetch; got %d targets, %d calls", len(targets), files.calls)
	}
}

func TestRouter_FetchErrorFallsBackToBase(t *testing.T) {
	m := &stubMappings{base: store.RepoMapping{SlackChannel: "C0BASE"}, hasPathRules: true, targets: []store.Target{{Channel: "C0A"}}}
	files := &stubFiles{err: errors.New("github down")}
	r := pullrequest.NewRouter(m, files, discardLogger())
	_, targets, err := r.ResolveTargets(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("should soft-fail: %v", err)
	}
	if len(targets) != 1 || targets[0].Channel != "C0BASE" {
		t.Fatalf("fetch error should fall back to base; got %+v", targets)
	}
}
```

(`stubFiles`, `discardLogger` already exist in the package's tests.)

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/pullrequest/ -run TestRouter`
Expected: FAIL — `ResolveTargets` undefined / `TargetsForFiles` missing from `PathMappings`.

- [ ] **Step 3: Rewrite the Router**

```go
// internal/pullrequest/router.go (replace Resolver/Resolve/PathMappings)

// PathMappings is the slice of the mappings provider the Router consumes.
type PathMappings interface {
	Get(ctx context.Context, repository string) (store.RepoMapping, error)
	RepoHasPathRules(repository string) bool
	TargetsForFiles(repository string, files []string) []store.Target
}

// ResolveTargets returns the per-repo behavior plus the fan-out targets for a
// PR. With no fetcher (no token) or no path rules it returns a single base
// target. A files-API error is soft: it logs and returns the base target.
func (r *Router) ResolveTargets(ctx context.Context, repository string, prNumber int) (store.RepoMapping, []store.Target, error) {
	behavior, err := r.mappings.Get(ctx, repository)
	if err != nil {
		return store.RepoMapping{}, nil, err
	}
	baseTarget := []store.Target{{Channel: behavior.SlackChannel, Mentions: behavior.Mentions}}

	if r.files == nil || !r.mappings.RepoHasPathRules(repository) {
		return behavior, baseTarget, nil
	}
	owner, repo, ok := splitOwnerRepo(repository)
	if !ok {
		return behavior, baseTarget, nil
	}
	files, err := r.files.ListPullRequestFiles(ctx, owner, repo, prNumber)
	if err != nil {
		r.logger.Warn("path routing: could not fetch changed files; routing to the repo tier",
			slog.String("repository", repository),
			slog.Int("pr", prNumber),
			slog.Any("err", err))
		return behavior, baseTarget, nil
	}
	return behavior, r.mappings.TargetsForFiles(repository, files), nil
}
```

Delete the old `Resolver` interface and `(*Router).Resolve`. Keep `Router`, `NewRouter`, `ChangedFiles`, `splitOwnerRepo`.

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/pullrequest/ -run TestRouter`
Expected: PASS. (`go build ./...` will still fail until handlers are updated — that is expected mid-cutover.)

- [ ] **Step 5: Commit**

```bash
git add internal/pullrequest/router.go internal/pullrequest/router_test.go
git commit -m "feat: Router.ResolveTargets returns fan-out targets"
```

### Task 8: Handler dependency interfaces

**Files:**
- Modify: `internal/pullrequest/deps.go`

**Interfaces:**
- Produces:
  - `PullRequestStore` (consumed by all handlers): `AddMessage(ctx, repository string, prNumber int, channel, messageID string) error`, `Messages(ctx, repository string, prNumber int) ([]store.Message, error)`, `Touch(...)`, `MarkClosed(...)`, `Delete(...)`.
  - `RepoBehavior` (close/draft/review): `Get(ctx, repository string) (store.RepoMapping, error)`.
  - `TargetResolver` (open): `ResolveTargets(ctx, repository string, prNumber int) (store.RepoMapping, []store.Target, error)`.
- Removes: `SlackMessages` interface (replaced by `PullRequestStore`).

- [ ] **Step 1: Replace the interfaces in `deps.go`**

```go
// internal/pullrequest/deps.go (replace SlackMessages with these; keep Messenger from Task 4)

// PullRequestStore persists tracked PRs and their per-channel messages.
type PullRequestStore interface {
	AddMessage(ctx context.Context, repository string, prNumber int, channel, messageID string) error
	Messages(ctx context.Context, repository string, prNumber int) ([]store.Message, error)
	Touch(ctx context.Context, repository string, prNumber int) error
	MarkClosed(ctx context.Context, repository string, prNumber int) error
	Delete(ctx context.Context, repository string, prNumber int) error
}

// RepoBehavior resolves a repository's per-repo behavioral config (reactions,
// review flags). Close/draft/review need it but not the per-channel targets.
type RepoBehavior interface {
	Get(ctx context.Context, repository string) (store.RepoMapping, error)
}

// TargetResolver resolves the open fan-out: per-repo behavior + per-channel targets.
type TargetResolver interface {
	ResolveTargets(ctx context.Context, repository string, prNumber int) (store.RepoMapping, []store.Target, error)
}
```

- [ ] **Step 2: Commit** (build still red mid-cutover; that's fine — handlers come next)

```bash
git add internal/pullrequest/deps.go
git commit -m "refactor: pullrequest store/behavior/target interfaces for fan-out"
```

### Task 9: Open handler fan-out

**Files:**
- Modify: `internal/pullrequest/open.go`
- Test: `internal/pullrequest/open_test.go`

**Interfaces:**
- Consumes: `PullRequestStore`, `TargetResolver`, `Messenger`, `*slack.Composer`.

- [ ] **Step 1: Write the failing test** — a PR with two targets posts two messages.

```go
// internal/pullrequest/open_test.go (add; adapt the existing fakes — see note)
func TestOpenHandler_FansOutToEachTarget(t *testing.T) {
	store := newFakePRStore()
	resolver := &fakeTargetResolver{
		behavior: storepkg.RepoMapping{Reactions: storepkg.Reactions{NewPR: "eyes"}},
		targets: []storepkg.Target{
			{Channel: "C0A", Mentions: []string{"<@U0A>"}},
			{Channel: "C0B", Mentions: []string{"<@U0B>"}},
		},
	}
	client := &fakeSlackClient{}
	h := pullrequest.NewOpenHandler(store, resolver, client, testComposer(), discardLogger())

	if err := h.Handle(context.Background(), openedEvent("acme/web", 7)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if posts := client.postsByChannel(); len(posts) != 2 || posts["C0A"] == 0 || posts["C0B"] == 0 {
		t.Fatalf("want one post per channel; got %+v", posts)
	}
	msgs, _ := store.Messages(context.Background(), "acme/web", 7)
	if len(msgs) != 2 {
		t.Fatalf("want 2 stored messages; got %d", len(msgs))
	}
}
```

**Note on fakes:** add to `internal/pullrequest/fakes_test.go` a `fakePRStore` implementing `PullRequestStore` (a `map[string][]store.Message` keyed by `repo#number`, with `AddMessage` deduping by channel), a `fakeTargetResolver{behavior, targets, err}` implementing `TargetResolver`, and on `fakeSlackClient` a `postsByChannel()` helper counting posts per channel. Replace the old `newFakeRepoMappings`/`Resolve`-based wiring used by the existing handler tests with these in this task and Tasks 10–12.

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/pullrequest/ -run TestOpenHandler_FansOut`
Expected: FAIL — constructor signature / behavior mismatch.

- [ ] **Step 3: Rewrite the open handler**

```go
// internal/pullrequest/open.go (replace struct, constructor, Handle)

type OpenHandler struct {
	store    PullRequestStore
	resolver TargetResolver
	slack    Messenger
	composer *slack.Composer
	logger   *slog.Logger
}

func NewOpenHandler(store PullRequestStore, resolver TargetResolver, slackClient Messenger, composer *slack.Composer, logger *slog.Logger) *OpenHandler {
	return &OpenHandler{store: store, resolver: resolver, slack: slackClient, composer: composer, logger: logger}
}

func (h *OpenHandler) Applicable(e Event) bool {
	if e.Action == "ready_for_review" {
		return true
	}
	return e.Action == "opened" && !e.PR.Draft
}

// Handle posts one message per resolved target channel and records each. It is
// idempotent per channel: an existing message for a channel is skipped, so a
// redelivery or a partial-failure retry only posts the missing channels.
func (h *OpenHandler) Handle(ctx context.Context, e Event) error {
	behavior, targets, err := h.resolver.ResolveTargets(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		h.logIgnored(e, "no_mapping")
		return nil
	}
	if err != nil {
		return err
	}

	existing, err := h.store.Messages(ctx, e.Repository, e.PR.Number)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	already := map[string]bool{}
	for _, m := range existing {
		already[m.Channel] = true
	}

	for _, target := range targets {
		if already[target.Channel] {
			continue
		}
		msg := h.composeMessage(e, behavior, target.Mentions)
		messageID, err := h.slack.PostMessage(ctx, target.Channel, msg)
		if err != nil {
			return err // successful channels are already saved; retry skips them
		}
		if err := h.store.AddMessage(ctx, e.Repository, e.PR.Number, target.Channel, messageID); err != nil {
			return err
		}
	}
	return nil
}

func (h *OpenHandler) composeMessage(e Event, behavior store.RepoMapping, mentions []string) slack.Message {
	if behavior.DependabotFormat {
		if kind := botpr.DetectBot(e.PR.Author); kind != botpr.BotKindNone {
			return h.composer.BotMessage(slackPRFrom(e), mentions, kind.Name(), botpr.IsSecurityAdvisory(e.PR.Body))
		}
	}
	return h.composer.NewMessage(slackPRFrom(e), mentions, behavior.Reactions.NewPR)
}

func (h *OpenHandler) logIgnored(e Event, reason string) {
	h.logger.Warn("ignored webhook event",
		slog.String("reason", reason),
		slog.String("handler", "open"),
		slog.String("github_event", e.GitHubEvent),
		slog.String("action", e.Action),
		slog.String("repository", e.Repository),
		slog.Int("pr", e.PR.Number),
	)
}
```

Keep `slackPRFrom` (shared). The old `already_sent` short-circuit is replaced by per-channel `already[...]` skipping.

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/pullrequest/ -run TestOpenHandler`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pullrequest/open.go internal/pullrequest/open_test.go internal/pullrequest/fakes_test.go
git commit -m "feat: open handler fans out one message per target channel"
```

### Task 10: Close handler reads stored messages

**Files:**
- Modify: `internal/pullrequest/close.go`
- Test: `internal/pullrequest/close_test.go`

**Interfaces:**
- Consumes: `PullRequestStore`, `RepoBehavior`, `Messenger`, `*slack.Composer`.

- [ ] **Step 1: Write the failing test** — close updates + reacts on every stored message.

```go
func TestCloseHandler_ActsOnEveryMessage(t *testing.T) {
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "acme/web", 7, "C0A", "100.1")
	_ = st.AddMessage(context.Background(), "acme/web", 7, "C0B", "200.1")
	behavior := &fakeBehavior{m: storepkg.RepoMapping{Reactions: storepkg.Reactions{Enabled: true, MergedPR: "tada"}}}
	client := &fakeSlackClient{}
	h := pullrequest.NewCloseHandler(st, behavior, client, testComposer(), discardLogger())

	if err := h.Handle(context.Background(), closedMergedEvent("acme/web", 7)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if client.updates() != 2 || client.reactions() != 2 {
		t.Fatalf("want 2 updates + 2 reactions; got %d / %d", client.updates(), client.reactions())
	}
}
```

(Add `fakeBehavior{ m store.RepoMapping }` implementing `RepoBehavior.Get` to `fakes_test.go`, plus `updates()`/`reactions()` counters on `fakeSlackClient`.)

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/pullrequest/ -run TestCloseHandler_ActsOnEvery`
Expected: FAIL — constructor signature mismatch.

- [ ] **Step 3: Rewrite the close handler**

```go
// internal/pullrequest/close.go (replace struct, constructor, Handle)

type CloseHandler struct {
	store    PullRequestStore
	behavior RepoBehavior
	slack    Messenger
	composer *slack.Composer
	logger   *slog.Logger
}

func NewCloseHandler(store PullRequestStore, behavior RepoBehavior, slackClient Messenger, composer *slack.Composer, logger *slog.Logger) *CloseHandler {
	return &CloseHandler{store: store, behavior: behavior, slack: slackClient, composer: composer, logger: logger}
}

func (h *CloseHandler) Applicable(e Event) bool { return e.Action == "closed" }

func (h *CloseHandler) Handle(ctx context.Context, e Event) error {
	messages, err := h.store.Messages(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		h.logIgnored(e, "no_stored_message")
		return nil
	}
	if err != nil {
		return err
	}
	behavior, err := h.behavior.Get(ctx, e.Repository)
	if errors.Is(err, store.ErrNotFound) {
		h.logIgnored(e, "no_mapping")
		return nil
	}
	if err != nil {
		return err
	}

	emoji := behavior.Reactions.ClosedPR
	if e.PR.Merged {
		emoji = behavior.Reactions.MergedPR
	}
	updated := h.composer.UpdatedMessage(slackPRFrom(e), e.PR.Merged, emoji)
	for _, m := range messages {
		if err := h.slack.UpdateMessage(ctx, m.Channel, m.MessageID, updated); err != nil {
			return err
		}
		if behavior.Reactions.Enabled {
			if err := h.slack.AddReaction(ctx, m.Channel, m.MessageID, emoji); err != nil {
				return err
			}
		}
	}
	return h.store.MarkClosed(ctx, e.Repository, e.PR.Number)
}

func (h *CloseHandler) logIgnored(e Event, reason string) {
	h.logger.Warn("ignored webhook event",
		slog.String("reason", reason), slog.String("handler", "close"),
		slog.String("github_event", e.GitHubEvent), slog.String("action", e.Action),
		slog.String("repository", e.Repository), slog.Int("pr", e.PR.Number))
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/pullrequest/ -run TestCloseHandler`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pullrequest/close.go internal/pullrequest/close_test.go internal/pullrequest/fakes_test.go
git commit -m "feat: close handler updates and reacts on every stored message"
```

### Task 11: Draft handler reads stored messages

**Files:**
- Modify: `internal/pullrequest/draft.go`
- Test: `internal/pullrequest/draft_test.go`

**Interfaces:**
- Consumes: `PullRequestStore`, `Messenger`. (Draft does not need behavior — it only deletes.)

- [ ] **Step 1: Write the failing test** — draft deletes every message and the PR row.

```go
func TestDraftHandler_DeletesEveryMessageAndRow(t *testing.T) {
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "acme/web", 7, "C0A", "100.1")
	_ = st.AddMessage(context.Background(), "acme/web", 7, "C0B", "200.1")
	client := &fakeSlackClient{}
	h := pullrequest.NewDraftHandler(st, client, discardLogger())

	if err := h.Handle(context.Background(), draftEvent("acme/web", 7)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if client.deletes() != 2 {
		t.Fatalf("want 2 deletes; got %d", client.deletes())
	}
	if _, err := st.Messages(context.Background(), "acme/web", 7); err != storepkg.ErrNotFound {
		t.Fatalf("PR row should be deleted; got %v", err)
	}
}
```

(Add `deletes()` counter to `fakeSlackClient`.)

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/pullrequest/ -run TestDraftHandler_DeletesEvery`
Expected: FAIL — constructor signature mismatch.

- [ ] **Step 3: Rewrite the draft handler**

```go
// internal/pullrequest/draft.go (replace struct, constructor, Handle)

type DraftHandler struct {
	store  PullRequestStore
	slack  Messenger
	logger *slog.Logger
}

func NewDraftHandler(store PullRequestStore, slackClient Messenger, logger *slog.Logger) *DraftHandler {
	return &DraftHandler{store: store, slack: slackClient, logger: logger}
}

func (h *DraftHandler) Applicable(e Event) bool { return e.Action == "converted_to_draft" }

func (h *DraftHandler) Handle(ctx context.Context, e Event) error {
	messages, err := h.store.Messages(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		h.logger.Info("ignored webhook event",
			slog.String("reason", "no_stored_message"), slog.String("handler", "draft"),
			slog.String("github_event", e.GitHubEvent), slog.String("action", e.Action),
			slog.String("repository", e.Repository), slog.Int("pr", e.PR.Number))
		return nil
	}
	if err != nil {
		return err
	}
	for _, m := range messages {
		if err := h.slack.DeleteMessage(ctx, m.Channel, m.MessageID); err != nil {
			return err
		}
	}
	return h.store.Delete(ctx, e.Repository, e.PR.Number)
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/pullrequest/ -run TestDraftHandler`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pullrequest/draft.go internal/pullrequest/draft_test.go internal/pullrequest/fakes_test.go
git commit -m "feat: draft handler deletes every stored message"
```

### Task 12: Review handlers read stored messages

**Files:**
- Modify: `internal/pullrequest/review_handlers.go`
- Test: `internal/pullrequest/review_handlers_test.go`

**Interfaces:**
- Consumes: `PullRequestStore`, `RepoBehavior`, `Messenger`, `*aireview.Detector`.

- [ ] **Step 1: Write the failing test** — a review reacts on every stored message and touches the PR.

```go
func TestReactionHandler_ReactsOnEveryMessage(t *testing.T) {
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "acme/web", 7, "C0A", "100.1")
	_ = st.AddMessage(context.Background(), "acme/web", 7, "C0B", "200.1")
	behavior := &fakeBehavior{m: storepkg.RepoMapping{Reactions: storepkg.Reactions{Approved: "white_check_mark"}}}
	client := &fakeSlackClient{}
	h := pullrequest.NewApproveHandler(st, behavior, client, discardLogger(), testDetector())

	if err := h.Handle(context.Background(), approvedEvent("acme/web", 7)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if client.reactions() != 2 || st.touched("acme/web", 7) != 1 {
		t.Fatalf("want 2 reactions + 1 touch; got %d / %d", client.reactions(), st.touched("acme/web", 7))
	}
}
```

(Add a `touched(repo, n)` counter to `fakePRStore`.)

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/pullrequest/ -run TestReactionHandler_ReactsOnEvery`
Expected: FAIL — constructor signature mismatch.

- [ ] **Step 3: Rewrite the reaction handler core** (the three constructors keep their `Applicable`/`emojiOf`; only deps change)

```go
// internal/pullrequest/review_handlers.go (replace the struct + Handle; update the three constructors' params)

type reactionHandler struct {
	name       string
	emojiOf    func(store.Reactions) string
	applicable func(Event) bool

	store    PullRequestStore
	behavior RepoBehavior
	slack    Messenger
	logger   *slog.Logger
	detector *aireview.Detector
}

func (h *reactionHandler) Applicable(e Event) bool { return h.applicable(e) }

func (h *reactionHandler) Handle(ctx context.Context, e Event) error {
	messages, err := h.store.Messages(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		h.logIgnored(e, "no_stored_message")
		return nil
	}
	if err != nil {
		return err
	}
	behavior, err := h.behavior.Get(ctx, e.Repository)
	if errors.Is(err, store.ErrNotFound) {
		h.logIgnored(e, "no_mapping")
		return nil
	}
	if err != nil {
		return err
	}
	if behavior.IgnoreAIReviews && h.detector.IsBot(e.Sender.Type) {
		h.logger.Debug("skipped bot reviewer reaction",
			slog.String("login", e.Sender.Login), slog.String("handler", h.name),
			slog.String("repository", e.Repository), slog.Int("pr", e.PR.Number))
		return nil
	}

	emoji := h.emojiOf(behavior.Reactions)
	for _, m := range messages {
		if err := h.slack.AddReaction(ctx, m.Channel, m.MessageID, emoji); err != nil {
			return err
		}
		if behavior.Reactions.BotReview != "" && h.detector.IsBot(e.Sender.Type) {
			if err := h.slack.AddReaction(ctx, m.Channel, m.MessageID, behavior.Reactions.BotReview); err != nil {
				return err
			}
		}
	}
	return h.store.Touch(ctx, e.Repository, e.PR.Number)
}

func (h *reactionHandler) logIgnored(e Event, reason string) {
	h.logger.Warn("ignored webhook event",
		slog.String("reason", reason), slog.String("handler", h.name),
		slog.String("github_event", e.GitHubEvent), slog.String("action", e.Action),
		slog.String("repository", e.Repository), slog.Int("pr", e.PR.Number))
}
```

Update the three constructors `NewApproveHandler`/`NewCommentedHandler`/`NewRequestChangeHandler` to the signature `(store PullRequestStore, behavior RepoBehavior, slackClient Messenger, logger *slog.Logger, detector *aireview.Detector)` and set `store: store, behavior: behavior, slack: slackClient, logger: logger, detector: detector` in each literal.

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/pullrequest/ -run TestReactionHandler`
Expected: PASS. Then `go test ./internal/pullrequest/` for the whole package.

- [ ] **Step 5: Commit**

```bash
git add internal/pullrequest/review_handlers.go internal/pullrequest/review_handlers_test.go internal/pullrequest/fakes_test.go
git commit -m "feat: review handlers react on every stored message"
```

### Task 13: Wire the new store in app.Wire

**Files:**
- Modify: `internal/app/app.go`

**Interfaces:**
- Consumes: `store.NewPullRequests`, the new handler constructors, `Router.ResolveTargets` via the `TargetResolver` interface.

- [ ] **Step 1: Replace the wiring**

In `app.Wire`, replace `messages := store.NewSlackMessages(db)` with `pullRequests := store.NewPullRequests(db)`. The `router` (built in #131) now satisfies `TargetResolver`. Update handler construction:

```go
dispatcher := pullrequest.NewDispatcher(
	logger,
	pullrequest.NewOpenHandler(pullRequests, router, slackClient, composer, logger),
	pullrequest.NewCloseHandler(pullRequests, provider, slackClient, composer, logger),
	pullrequest.NewDraftHandler(pullRequests, slackClient, logger),
	pullrequest.NewApproveHandler(pullRequests, provider, slackClient, logger, aiDetector),
	pullrequest.NewCommentedHandler(pullRequests, provider, slackClient, logger, aiDetector),
	pullrequest.NewRequestChangeHandler(pullRequests, provider, slackClient, logger, aiDetector),
)
```

The digest reporter (Task 14) and cleanup (Task 15) constructions also change to take `pullRequests`; update them in the same edit so the file compiles.

- [ ] **Step 2: Build** — expect remaining errors only in digest/cleanup until Tasks 14–15; if wiring those in the same edit, `go build ./internal/app/` should pass after Task 15.

- [ ] **Step 3: Commit**

```bash
git add internal/app/app.go
git commit -m "feat: wire PullRequests store and fan-out handlers"
```

### Task 14: Digest groups by stored message channel

**Files:**
- Modify: `internal/digest/reporter.go`
- Test: `internal/digest/reporter_test.go`

**Interfaces:**
- Consumes: `StuckFinder.FindStuck(ctx, cutoff) ([]store.PullRequest, error)` (messages preloaded); existing `MappingLookup.Get`, `Poster`.
- Produces: grouping by `message.channel`; per-channel mentions = base mentions only when the channel equals the repo's base channel.

- [ ] **Step 1: Update the `StuckFinder` interface and write the failing test**

```go
// internal/digest/reporter_test.go (add)
func TestDigest_GroupsByStoredMessageChannel(t *testing.T) {
	finder := &fakeFinder{prs: []storepkg.PullRequest{{
		Repository: "acme/mono", PRNumber: 7, UpdatedAt: longAgo,
		Messages: []storepkg.Message{{Channel: "C0BASE", MessageID: "1"}, {Channel: "C0AUTH", MessageID: "2"}},
	}}}
	mappings := &fakeMappings{base: storepkg.RepoMapping{SlackChannel: "C0BASE", Mentions: []string{"<!subteam^S0ENG>"}}}
	poster := &fakePoster{}
	r := digest.NewReporter(finder, mappings, poster, testComposer(), enabledResolver(), discardLogger(), time.UTC)

	r.Run(context.Background())
	posted := poster.channels()
	if !posted["C0BASE"] || !posted["C0AUTH"] {
		t.Fatalf("want a reminder in each stored channel; got %+v", posted)
	}
	// Base mentions only on the base channel.
	if got := poster.mentionsFor("C0AUTH"); len(got) != 0 {
		t.Errorf("path channel should get no ping without stored mentions; got %v", got)
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/digest/ -run TestDigest_GroupsByStored`
Expected: FAIL — `FindStuck` returns the wrong type / grouping not implemented.

- [ ] **Step 3: Rewrite `groupByChannel` and the `StuckFinder` interface**

```go
// internal/digest/reporter.go

// StuckFinder returns open PRs (with their messages) idle since before cutoff.
type StuckFinder interface {
	FindStuck(ctx context.Context, cutoff time.Time) ([]store.PullRequest, error)
}
```

Rewrite `groupByChannel` to iterate each PR's `Messages`, bucket by `message.Channel`, build each `StuckPR` from `pr.Repository`/`pr.PRNumber`/`pr.UpdatedAt`, and add the repo's base `Mentions` (from `mappings.Get`) to a channel group **only when that channel equals the repo's base `SlackChannel`**:

```go
func (r *Reporter) groupByChannel(ctx context.Context, prs []store.PullRequest, now time.Time, include func(repo string) bool) []channelGroup {
	var order []string
	byChannel := map[string]*channelGroup{}
	mentionSeen := map[string]map[string]bool{}

	for _, pr := range prs {
		if !include(pr.Repository) {
			continue
		}
		mapping, err := r.mappings.Get(ctx, pr.Repository)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			r.logger.Error("stuck-pr digest: mapping lookup failed",
				slog.String("repository", pr.Repository), slog.Any("err", err))
			continue
		}
		for _, m := range pr.Messages {
			g := byChannel[m.Channel]
			if g == nil {
				g = &channelGroup{channel: m.Channel}
				byChannel[m.Channel] = g
				mentionSeen[m.Channel] = map[string]bool{}
				order = append(order, m.Channel)
			}
			if m.Channel == mapping.SlackChannel { // base channel → ping base mentions
				for _, mention := range mapping.Mentions {
					if !mentionSeen[m.Channel][mention] {
						mentionSeen[m.Channel][mention] = true
						g.mentions = append(g.mentions, mention)
					}
				}
			}
			g.prs = append(g.prs, slack.StuckPR{
				Repository: pr.Repository, Number: pr.PRNumber,
				URL: prURL(pr.Repository, pr.PRNumber), IdleDays: idleDays(now, pr.UpdatedAt),
			})
		}
	}

	out := make([]channelGroup, 0, len(order))
	for _, ch := range order {
		out = append(out, *byChannel[ch])
	}
	return out
}
```

Update the `groupByChannel` callers in `reporter.go` to pass `[]store.PullRequest` (rename the local `rows` to `prs`).

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/digest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/digest/reporter.go internal/digest/reporter_test.go
git commit -m "feat: digest groups stuck PRs by stored message channel"
```

### Task 15: Cleanup and reconcile on PullRequests

**Files:**
- Modify: `internal/cleanup/cleanup.go` (interface doc only — signature is unchanged: `DeleteStaleBefore`)
- Modify: `internal/reconcile/reconcile.go`
- Test: `internal/reconcile/reconcile_test.go`

**Interfaces:**
- Consumes: `store.PullRequest` instead of `store.SlackMessage` in `OpenLister.ListOpen`.

- [ ] **Step 1: Update `reconcile` types + a failing test**

```go
// internal/reconcile/reconcile.go
type OpenLister interface {
	ListOpen(ctx context.Context) ([]store.PullRequest, error)
}
```

Change the three helpers `markClosed`/`removeNotFound`/`removeDraft` to take `row store.PullRequest` and use `row.Repository`/`row.PRNumber` (same field names — no body changes beyond the type). Add/adjust a test asserting an open `store.PullRequest` with a 404 PR is marked closed (mirror the existing `slack_messages` test, swapping the type).

`cleanup` needs no code change — `DeleteStaleBefore(ctx, cutoff) (int64, error)` is satisfied by `*store.PullRequests`; update the interface doc comment to say "stale pull_requests" and confirm the test's fake still matches.

- [ ] **Step 2: Run tests, verify the new one fails then passes after the edit**

Run: `env -u ... go test ./internal/reconcile/ ./internal/cleanup/`
Expected: PASS after the type swap.

- [ ] **Step 3: Commit**

```bash
git add internal/reconcile/ internal/cleanup/
git commit -m "feat: reconcile and cleanup operate on pull_requests"
```

### Task 16: Drop the old table and repository

**Files:**
- Create: `internal/store/migrations/00006_drop_slack_messages.sql`
- Delete: `internal/store/slack_messages.go`
- Modify: `internal/store/models.go` (remove `SlackMessage`), `internal/store/store_test.go`/`models_test.go` (remove `SlackMessage` tests)

**Interfaces:**
- Removes: `store.SlackMessages`, `store.SlackMessage`.

- [ ] **Step 1: Write the migration**

```sql
-- internal/store/migrations/00006_drop_slack_messages.sql
-- +goose Up
DROP TABLE slack_messages;

-- +goose Down
CREATE TABLE slack_messages (
    pr_number     INTEGER NOT NULL,
    gh_repository TEXT    NOT NULL,
    ts            TEXT    NOT NULL,
    updated_at    DATETIME,
    closed_at     DATETIME,
    PRIMARY KEY (pr_number, gh_repository)
);
```

- [ ] **Step 2: Delete `internal/store/slack_messages.go`** and remove the `SlackMessage` struct + `TableName` from `models.go` and any `SlackMessage`-specific tests.

- [ ] **Step 3: Build + full test**

Run: `env -u ... go build ./... && env -u ... go test -race ./...`
Expected: PASS — no remaining references to `SlackMessage(s)`.

- [ ] **Step 4: Commit**

```bash
git add internal/store/
git commit -m "feat: drop slack_messages table and repository"
```

### Task 17: Docs and upgrade warning

**Files:**
- Modify: `docs/mappings.md` — change the per-path "Routing behavior" section from single-winner to fan-out (one message per matched channel; mentions unioned per channel; base only as no-match fallback; remove the M5/safety-valve and @channel single-winner wording).
- Modify: `docs/operations.md` — note a PR may now have multiple Slack messages; the digest reminds in each stored channel.
- Modify: `docs/upgrading.md` — add a prominent warning for this release.

- [ ] **Step 1: Write the upgrade warning** (verbatim content, not a placeholder)

```markdown
## Upgrading to multi-message fan-out

This release replaces the single `slack_messages` table with `pull_requests` and `messages`. **The migration drops `slack_messages` — all in-flight PR tracking is lost on upgrade.** PRs opened before the upgrade keep their existing Slack messages but receive no further updates, reactions, or digest entries; PRs opened after the upgrade are unaffected. No action is required beyond the normal migration; this is a one-time, self-healing cutover.
```

- [ ] **Step 2: Update `docs/mappings.md` and `docs/operations.md`** to describe fan-out (one message per matched channel) and the digest's per-channel reminders, replacing single-winner language.

- [ ] **Step 3: Verify links/format** — `rg -n "single-winner|most-specific winning|safety valve" docs/` returns nothing stale.

- [ ] **Step 4: Commit**

```bash
git add docs/
git commit -m "docs: fan-out routing, multi-message digest, and upgrade warning"
```

### Task 18: Open PR 2

- [ ] Full verification: `env -u ... go vet ./... && env -u ... golangci-lint run ./... && env -u ... go test -race ./... && go build ./...`.
- [ ] Push; open PR titled `feat: per-PR multi-message fan-out across path channels`, body from template, label `enhancement`, assignee `@me`. Body must restate the upgrade-data-loss warning.

---

## Self-Review

**Spec coverage:**
- Schema (`pull_requests`, `messages`, cascade, unique keys, `created_at`) → Tasks 1, 2.
- Messenger abstraction (`Messenger`, `message_id`) → Task 4 + models in Task 1.
- Split behavior/targets + fan-out resolution (no cap, M1/M5 removed) → Tasks 6, 7.
- Open per-channel idempotent fan-out → Task 9.
- Lifecycle handlers read stored messages → Tasks 10, 11, 12.
- Digest by stored channel + base-mentions-only-on-base-channel → Task 14.
- Cleanup/reconcile PR-level → Task 15.
- Clean-cutover migration dropping `slack_messages` + loud upgrade warning → Tasks 2, 16, 17.
- Wiring → Task 13.

**Placeholders:** none — every code step shows the code; the only intentional "adapt the fakes" note (Task 9) is accompanied by the exact fakes to add.

**Type consistency:** `AddMessage`/`Messages`/`Touch`/`MarkClosed`/`Delete` signatures match across `store.PullRequests` (Task 3), the `PullRequestStore` interface (Task 8), and handler call sites (Tasks 9–12). `ResolveTargets` returns `(store.RepoMapping, []store.Target, error)` consistently in Tasks 7, 8, 9, 13. `TargetsForFiles(repository, files) []store.Target` matches across Tasks 6, 7. `FindStuck` returns `[]store.PullRequest` in Tasks 3, 14. `ListOpen` returns `[]store.PullRequest` in Tasks 3, 15.

**Note on test imports:** several test snippets reference the store package as `storepkg` for clarity; use whatever alias the existing test file already uses (`store_test` files import it as `store`). Keep one alias per file.
