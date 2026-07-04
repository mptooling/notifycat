package infrastructure

import (
	"context"
	"testing"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func TestMessageChecker_HasMessages_TrueForSeededPR(t *testing.T) {
	db := persistence.NewTestDB(t)
	ctx := context.Background()

	pullRequests := persistence.NewPullRequests(db)
	checker := NewMessageChecker(pullRequests)

	const (
		repository = "octo/widget"
		prNumber   = 10
	)

	if err := pullRequests.AddMessage(ctx, repository, prNumber, "C001", "ts-1"); err != nil {
		t.Fatalf("seed pull request: %v", err)
	}

	hasMessages, err := checker.HasMessages(ctx, repository, prNumber)
	if err != nil {
		t.Fatalf("HasMessages: %v", err)
	}
	if !hasMessages {
		t.Error("HasMessages = false for a seeded PR; want true")
	}
}

func TestMessageChecker_HasMessages_FalseForUntrackedPR(t *testing.T) {
	db := persistence.NewTestDB(t)
	ctx := context.Background()

	pullRequests := persistence.NewPullRequests(db)
	checker := NewMessageChecker(pullRequests)

	hasMessages, err := checker.HasMessages(ctx, "octo/widget", 99)
	if err != nil {
		t.Fatalf("HasMessages for untracked PR: %v", err)
	}
	if hasMessages {
		t.Error("HasMessages = true for an untracked PR; want false")
	}
}
