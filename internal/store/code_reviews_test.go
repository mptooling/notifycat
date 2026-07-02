package store_test

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/store"
)

// seedPR inserts a tracked PR (via the normal open path) so a code review has a
// parent row to reference, returning the store bound to the same db.
func seedPR(t *testing.T, db *gorm.DB, repository string, prNumber int) {
	t.Helper()
	if err := store.NewPullRequests(db).AddMessage(context.Background(), repository, prNumber, "C0", "1.1"); err != nil {
		t.Fatalf("seed pull request: %v", err)
	}
}

func TestCodeReviews_StartThenSecondStartConflicts(t *testing.T) {
	db := store.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := store.NewCodeReviews(db)
	ctx := context.Background()

	if err := reviews.Start(ctx, "acme/web", 7, "U1", "Ada"); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := reviews.Start(ctx, "acme/web", 7, "U2", "Bo"); err != store.ErrActiveReviewExists {
		t.Fatalf("second Start = %v; want ErrActiveReviewExists", err)
	}
}

func TestCodeReviews_FinishAllowsRestart(t *testing.T) {
	db := store.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := store.NewCodeReviews(db)
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
	db := store.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := store.NewCodeReviews(db)
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
	if _, err := reviews.GetActive(ctx, "acme/web", 7); err != store.ErrNotFound {
		t.Fatalf("GetActive after Finish = %v; want ErrNotFound", err)
	}
}

func TestCodeReviews_GetActiveNotFound(t *testing.T) {
	db := store.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := store.NewCodeReviews(db)
	if _, err := reviews.GetActive(context.Background(), "acme/web", 7); err != store.ErrNotFound {
		t.Fatalf("GetActive with no review = %v; want ErrNotFound", err)
	}
}

func TestCodeReviews_FinishNoActiveIsNoop(t *testing.T) {
	db := store.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := store.NewCodeReviews(db)
	if err := reviews.Finish(context.Background(), "acme/web", 7); err != nil {
		t.Fatalf("Finish with no active review = %v; want no-op nil", err)
	}
}

func TestCodeReviews_StartUnknownPRNotFound(t *testing.T) {
	db := store.NewTestDB(t)
	reviews := store.NewCodeReviews(db)
	if err := reviews.Start(context.Background(), "acme/web", 999, "U1", "Ada"); err != store.ErrNotFound {
		t.Fatalf("Start on untracked PR = %v; want ErrNotFound", err)
	}
}

func TestCodeReviews_CascadeDeletedWithPR(t *testing.T) {
	db := store.NewTestDB(t)
	seedPR(t, db, "acme/web", 7)
	reviews := store.NewCodeReviews(db)
	ctx := context.Background()
	if err := reviews.Start(ctx, "acme/web", 7, "U1", "Ada"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := store.NewPullRequests(db).Delete(ctx, "acme/web", 7); err != nil {
		t.Fatalf("Delete PR: %v", err)
	}
	var count int64
	db.Raw("SELECT count(*) FROM code_reviews").Scan(&count)
	if count != 0 {
		t.Fatalf("code_reviews not cascade-deleted; count=%d", count)
	}
}

func TestCodeReviews_MigrationDownReverses(t *testing.T) {
	db := store.NewTestDB(t) // all migrations applied
	ctx := context.Background()
	if err := store.MigrateDown(ctx, db); err != nil {
		t.Fatalf("MigrateDown: %v", err)
	}
	var name string
	db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name='code_reviews'").Scan(&name)
	if name != "" {
		t.Fatalf("code_reviews still present after Down; got %q", name)
	}
}
