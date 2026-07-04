package persistence_test

import (
	"context"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func TestPullRequests_AddMessageIsIdempotentPerChannel(t *testing.T) {
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))
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
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))
	if _, err := repo.Messages(context.Background(), "acme/web", 999); err != persistence.ErrNotFound {
		t.Fatalf("want ErrNotFound; got %v", err)
	}
}

func TestPullRequests_DeleteCascadesMessages(t *testing.T) {
	db := persistence.NewTestDB(t)
	repo := persistence.NewPullRequests(db)
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
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))
	ctx := context.Background()
	_ = repo.AddMessage(ctx, "acme/web", 7, "C0A", "100.1")
	_ = repo.Touch(ctx, "acme/web", 7)
	// A far-future cutoff returns the (recently-touched) PR so we can assert its
	// messages were preloaded.
	stuck, err := repo.FindStuck(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("FindStuck: %v", err)
	}
	if len(stuck) != 1 || len(stuck[0].Messages) != 1 || stuck[0].Messages[0].Channel != "C0A" {
		t.Fatalf("FindStuck = %+v; want one PR with its message preloaded", stuck)
	}
}
