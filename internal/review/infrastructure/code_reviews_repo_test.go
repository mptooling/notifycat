package infrastructure

import (
	"context"
	"errors"
	"testing"

	notificationdomain "github.com/mptooling/notifycat/internal/notification/domain"
	reviewdomain "github.com/mptooling/notifycat/internal/review/domain"
	"github.com/mptooling/notifycat/internal/store"
)

func TestCodeReviewsRepo_HasActiveReview_FalseWhenNone(t *testing.T) {
	db := store.NewTestDB(t)
	ctx := context.Background()

	pullRequests := store.NewPullRequests(db)
	codeReviews := store.NewCodeReviews(db)
	repo := NewCodeReviewsRepo(codeReviews)

	const (
		repository  = "octo/widget"
		prNumber    = 1
		slackUserID = "U001"
	)

	if err := pullRequests.AddMessage(ctx, repository, prNumber, "C001", "ts-1"); err != nil {
		t.Fatalf("seed pull request: %v", err)
	}

	hasActive, err := repo.HasActiveReview(ctx, repository, prNumber, slackUserID)
	if err != nil {
		t.Fatalf("HasActiveReview: %v", err)
	}
	if hasActive {
		t.Error("HasActiveReview = true; want false before any Start")
	}

	if err := codeReviews.Start(ctx, repository, prNumber, slackUserID, "alice"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	hasActive, err = repo.HasActiveReview(ctx, repository, prNumber, slackUserID)
	if err != nil {
		t.Fatalf("HasActiveReview after Start: %v", err)
	}
	if !hasActive {
		t.Error("HasActiveReview = false after Start; want true")
	}
}

func TestCodeReviewsRepo_Start_DuplicateReturnsErrActiveReviewExists(t *testing.T) {
	db := store.NewTestDB(t)
	ctx := context.Background()

	pullRequests := store.NewPullRequests(db)
	codeReviews := store.NewCodeReviews(db)
	repo := NewCodeReviewsRepo(codeReviews)

	const (
		repository    = "octo/widget"
		prNumber      = 2
		slackUserID   = "U002"
		slackUserName = "bob"
	)

	if err := pullRequests.AddMessage(ctx, repository, prNumber, "C001", "ts-2"); err != nil {
		t.Fatalf("seed pull request: %v", err)
	}

	if err := repo.Start(ctx, repository, prNumber, slackUserID, slackUserName); err != nil {
		t.Fatalf("first Start: %v", err)
	}

	err := repo.Start(ctx, repository, prNumber, slackUserID, slackUserName)
	if !errors.Is(err, reviewdomain.ErrActiveReviewExists) {
		t.Fatalf("second Start error = %v; want reviewdomain.ErrActiveReviewExists", err)
	}
}

func TestCodeReviewsRepo_GetActive_ReturnsSessionAndErrNoActiveReview(t *testing.T) {
	db := store.NewTestDB(t)
	ctx := context.Background()

	pullRequests := store.NewPullRequests(db)
	codeReviews := store.NewCodeReviews(db)
	repo := NewCodeReviewsRepo(codeReviews)

	const (
		repository    = "octo/widget"
		prNumber      = 3
		slackUserID   = "U003"
		slackUserName = "carol"
	)

	_, err := repo.GetActive(ctx, repository, prNumber)
	if !errors.Is(err, notificationdomain.ErrNoActiveReview) {
		t.Fatalf("GetActive with no session: error = %v; want notificationdomain.ErrNoActiveReview", err)
	}

	if err := pullRequests.AddMessage(ctx, repository, prNumber, "C001", "ts-3"); err != nil {
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

func TestCodeReviewsRepo_Reviewers_MapsAllSessions(t *testing.T) {
	db := store.NewTestDB(t)
	ctx := context.Background()

	pullRequests := store.NewPullRequests(db)
	codeReviews := store.NewCodeReviews(db)
	repo := NewCodeReviewsRepo(codeReviews)

	const (
		repository = "octo/widget"
		prNumber   = 4
	)

	if err := pullRequests.AddMessage(ctx, repository, prNumber, "C001", "ts-4"); err != nil {
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
