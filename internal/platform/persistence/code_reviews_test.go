package persistence_test

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// seedPR inserts a tracked PR (via the normal open path) so a code review has a
// parent row to reference, returning the store bound to the same db.
func seedPR(t *testing.T, db *gorm.DB, repository string, prNumber int) {
	t.Helper()
	if err := persistence.NewPullRequests(db).AddMessage(context.Background(), repository, prNumber, "C0", "1.1"); err != nil {
		t.Fatalf("seed pull request: %v", err)
	}
}

func TestCodeReviews_SameUserSecondStartConflicts(t *testing.T) {
	db := persistence.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := persistence.NewCodeReviews(db)
	ctx := context.Background()

	if err := reviews.Start(ctx, "acme/web", 7, "U1", "Ada"); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := reviews.Start(ctx, "acme/web", 7, "U1", "Ada"); err != persistence.ErrActiveReviewExists {
		t.Fatalf("same-user second Start = %v; want ErrActiveReviewExists", err)
	}
}

func TestCodeReviews_DifferentUsersReviewConcurrently(t *testing.T) {
	db := persistence.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := persistence.NewCodeReviews(db)
	ctx := context.Background()

	if err := reviews.Start(ctx, "acme/web", 7, "U1", "Ada"); err != nil {
		t.Fatalf("first reviewer: %v", err)
	}
	if err := reviews.Start(ctx, "acme/web", 7, "U2", "Bo"); err != nil {
		t.Fatalf("second, different reviewer should be allowed: %v", err)
	}
}

func TestCodeReviews_FinishAllowsRestart(t *testing.T) {
	db := persistence.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := persistence.NewCodeReviews(db)
	ctx := context.Background()

	if err := reviews.Start(ctx, "acme/web", 7, "U1", "Ada"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := reviews.Finish(ctx, "acme/web", 7); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if err := reviews.Start(ctx, "acme/web", 7, "U2", "Bo"); err != nil {
		t.Fatalf("restart after Finish: %v", err)
	}
}

func TestCodeReviews_GetActive(t *testing.T) {
	db := persistence.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := persistence.NewCodeReviews(db)
	ctx := context.Background()

	if err := reviews.Start(ctx, "acme/web", 7, "U1", "Ada"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	active, err := reviews.GetActive(ctx, "acme/web", 7)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if active.SlackUserID != "U1" || active.SlackUserName != "Ada" || active.FinishedAt != nil {
		t.Fatalf("GetActive = %+v; want open review by U1/Ada", active)
	}

	if err := reviews.Finish(ctx, "acme/web", 7); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if _, err := reviews.GetActive(ctx, "acme/web", 7); err != persistence.ErrNotFound {
		t.Fatalf("GetActive after Finish = %v; want ErrNotFound", err)
	}
}

func TestCodeReviews_GetActiveNotFound(t *testing.T) {
	db := persistence.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := persistence.NewCodeReviews(db)
	if _, err := reviews.GetActive(context.Background(), "acme/web", 7); err != persistence.ErrNotFound {
		t.Fatalf("GetActive with no review = %v; want ErrNotFound", err)
	}
}

func TestCodeReviews_ActiveForUser(t *testing.T) {
	db := persistence.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := persistence.NewCodeReviews(db)
	ctx := context.Background()

	if err := reviews.Start(ctx, "acme/web", 7, "U1", "Ada"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got, err := reviews.ActiveForUser(ctx, "acme/web", 7, "U1")
	if err != nil || got.SlackUserID != "U1" {
		t.Fatalf("ActiveForUser(U1) = %+v, %v; want U1's active review", got, err)
	}
	if _, err := reviews.ActiveForUser(ctx, "acme/web", 7, "U2"); err != persistence.ErrNotFound {
		t.Fatalf("ActiveForUser(U2) = %v; want ErrNotFound", err)
	}
	if err := reviews.Finish(ctx, "acme/web", 7); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if _, err := reviews.ActiveForUser(ctx, "acme/web", 7, "U1"); err != persistence.ErrNotFound {
		t.Fatalf("ActiveForUser(U1) after Finish = %v; want ErrNotFound", err)
	}
}

func TestCodeReviews_FinishNoActiveIsNoop(t *testing.T) {
	db := persistence.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := persistence.NewCodeReviews(db)
	if err := reviews.Finish(context.Background(), "acme/web", 7); err != nil {
		t.Fatalf("Finish with no active review = %v; want no-op nil", err)
	}
}

func TestCodeReviews_StartUnknownPRNotFound(t *testing.T) {
	db := persistence.NewTestDB(t)
	reviews := persistence.NewCodeReviews(db)
	if err := reviews.Start(context.Background(), "acme/web", 999, "U1", "Ada"); err != persistence.ErrNotFound {
		t.Fatalf("Start on untracked PR = %v; want ErrNotFound", err)
	}
}

func TestCodeReviews_CascadeDeletedWithPR(t *testing.T) {
	db := persistence.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := persistence.NewCodeReviews(db)
	ctx := context.Background()
	if err := reviews.Start(ctx, "acme/web", 7, "U1", "Ada"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := persistence.NewPullRequests(db).Delete(ctx, "acme/web", 7); err != nil {
		t.Fatalf("Delete PR: %v", err)
	}
	var count int64
	db.Raw("SELECT count(*) FROM code_reviews").Scan(&count)
	if count != 0 {
		t.Fatalf("code_reviews not cascade-deleted; count=%d", count)
	}
}

func TestCodeReviews_Reviewers(t *testing.T) {
	db := persistence.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	seedPR(t, db, "acme/web", 8)
	reviews := persistence.NewCodeReviews(db)
	ctx := context.Background()

	if err := reviews.Start(ctx, "acme/web", 7, "U1", "Ada"); err != nil {
		t.Fatalf("Start U1: %v", err)
	}
	if err := reviews.Finish(ctx, "acme/web", 7); err != nil {
		t.Fatalf("Finish U1: %v", err)
	}
	if err := reviews.Start(ctx, "acme/web", 7, "U2", "Bo"); err != nil {
		t.Fatalf("Start U2: %v", err)
	}
	// PR 8 has a separate reviewer that should not appear for PR 7.
	if err := reviews.Start(ctx, "acme/web", 8, "U3", "Cy"); err != nil {
		t.Fatalf("Start U3 on PR8: %v", err)
	}

	got, err := reviews.Reviewers(ctx, "acme/web", 7)
	if err != nil {
		t.Fatalf("Reviewers: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 reviews for PR 7; got %d: %+v", len(got), got)
	}
	if got[0].SlackUserID != "U1" || got[1].SlackUserID != "U2" {
		t.Errorf("reviews not in started_at ASC order: %+v", got)
	}
}

func TestCodeReviews_Reviewers_UntrackedPR(t *testing.T) {
	db := persistence.NewTestDB(t)
	reviews := persistence.NewCodeReviews(db)
	got, err := reviews.Reviewers(context.Background(), "acme/web", 999)
	if err != nil {
		t.Fatalf("Reviewers on untracked PR = %v; want nil error", err)
	}
	if len(got) != 0 {
		t.Fatalf("Reviewers on untracked PR should be empty; got %+v", got)
	}
}

func TestCodeReviews_Migration00008DownRestoresSingleActiveIndex(t *testing.T) {
	db := persistence.NewTestDB(t) // all migrations applied → per-(PR,user) index
	ctx := context.Background()

	var upSQL string
	db.Raw("SELECT sql FROM sqlite_master WHERE type='index' AND name='idx_code_reviews_active'").Scan(&upSQL)
	if !strings.Contains(upSQL, "slack_user_id") {
		t.Fatalf("expected per-(PR,user) index after up; got %q", upSQL)
	}

	if err := persistence.MigrateDown(ctx, db); err != nil {
		t.Fatalf("MigrateDown: %v", err)
	}

	var downSQL string
	db.Raw("SELECT sql FROM sqlite_master WHERE type='index' AND name='idx_code_reviews_active'").Scan(&downSQL)
	if strings.Contains(downSQL, "slack_user_id") {
		t.Fatalf("00008 Down should restore the single-active index; got %q", downSQL)
	}
}
