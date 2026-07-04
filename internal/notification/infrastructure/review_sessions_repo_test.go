package infrastructure

import (
	"context"
	"errors"
	"testing"

	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/store"
)

func TestReviewSessionsRepo_GetActive_ReturnsSession(t *testing.T) {
	db := store.NewTestDB(t)
	ctx := context.Background()

	pullRequests := store.NewPullRequests(db)
	codeReviews := store.NewCodeReviews(db)
	repo := NewReviewSessionsRepo(codeReviews)

	const (
		repository    = "octo/widget"
		prNumber      = 7
		slackUserID   = "U12345"
		slackUserName = "alice"
	)

	if err := pullRequests.AddMessage(ctx, repository, prNumber, "C001", "ts-1"); err != nil {
		t.Fatalf("seed pull request: %v", err)
	}
	if err := codeReviews.Start(ctx, repository, prNumber, slackUserID, slackUserName); err != nil {
		t.Fatalf("Start: %v", err)
	}

	session, err := repo.GetActive(ctx, repository, prNumber)
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if session.SlackUserID != slackUserID {
		t.Errorf("SlackUserID = %q; want %q", session.SlackUserID, slackUserID)
	}
	if session.SlackUserName != slackUserName {
		t.Errorf("SlackUserName = %q; want %q", session.SlackUserName, slackUserName)
	}
}

func TestReviewSessionsRepo_GetActive_NoSession_ReturnsErrNoActiveReview(t *testing.T) {
	db := store.NewTestDB(t)
	ctx := context.Background()

	codeReviews := store.NewCodeReviews(db)
	repo := NewReviewSessionsRepo(codeReviews)

	_, err := repo.GetActive(ctx, "octo/widget", 99)
	if !errors.Is(err, domain.ErrNoActiveReview) {
		t.Fatalf("GetActive error = %v; want domain.ErrNoActiveReview", err)
	}
}

func TestReviewSessionsRepo_Reviewers_MapsAllSessions(t *testing.T) {
	db := store.NewTestDB(t)
	ctx := context.Background()

	pullRequests := store.NewPullRequests(db)
	codeReviews := store.NewCodeReviews(db)
	repo := NewReviewSessionsRepo(codeReviews)

	const (
		repository = "octo/widget"
		prNumber   = 3
	)

	if err := pullRequests.AddMessage(ctx, repository, prNumber, "C001", "ts-1"); err != nil {
		t.Fatalf("seed pull request: %v", err)
	}

	reviewers := []struct {
		slackUserID   string
		slackUserName string
	}{
		{"U111", "alice"},
		{"U222", "bob"},
	}

	for _, reviewer := range reviewers {
		if err := codeReviews.Start(ctx, repository, prNumber, reviewer.slackUserID, reviewer.slackUserName); err != nil {
			t.Fatalf("Start %s: %v", reviewer.slackUserID, err)
		}
		// Finish to allow the next reviewer to start (partial unique index on active).
		if err := codeReviews.Finish(ctx, repository, prNumber); err != nil {
			t.Fatalf("Finish %s: %v", reviewer.slackUserID, err)
		}
	}

	sessions, err := repo.Reviewers(ctx, repository, prNumber)
	if err != nil {
		t.Fatalf("Reviewers: %v", err)
	}
	if len(sessions) != len(reviewers) {
		t.Fatalf("Reviewers returned %d sessions; want %d", len(sessions), len(reviewers))
	}
	for i, reviewer := range reviewers {
		if sessions[i].SlackUserID != reviewer.slackUserID {
			t.Errorf("sessions[%d].SlackUserID = %q; want %q", i, sessions[i].SlackUserID, reviewer.slackUserID)
		}
		if sessions[i].SlackUserName != reviewer.slackUserName {
			t.Errorf("sessions[%d].SlackUserName = %q; want %q", i, sessions[i].SlackUserName, reviewer.slackUserName)
		}
	}
}
