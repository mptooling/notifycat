package infrastructure

import (
	"context"
	"errors"

	notificationdomain "github.com/mptooling/notifycat/internal/notification/domain"
	reviewdomain "github.com/mptooling/notifycat/internal/review/domain"
	"github.com/mptooling/notifycat/internal/store"
)

// CodeReviewsRepo adapts the store's CodeReviews repository to the review
// domain's Recorder port and — because review owns review sessions — to the
// notification ReviewSessions port the notification handlers consume. No
// gorm-tagged type crosses a port.
type CodeReviewsRepo struct {
	codeReviews *store.CodeReviews
}

// NewCodeReviewsRepo wraps the store's CodeReviews repository.
func NewCodeReviewsRepo(codeReviews *store.CodeReviews) *CodeReviewsRepo {
	return &CodeReviewsRepo{codeReviews: codeReviews}
}

// HasActiveReview implements reviewdomain.Recorder.
func (r *CodeReviewsRepo) HasActiveReview(ctx context.Context, repository string, prNumber int, userID string) (bool, error) {
	_, err := r.codeReviews.ActiveForUser(ctx, repository, prNumber, userID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Start implements reviewdomain.Recorder, mapping the store's active-exists
// sentinel to the review domain's.
func (r *CodeReviewsRepo) Start(ctx context.Context, repository string, prNumber int, userID, userName string) error {
	err := r.codeReviews.Start(ctx, repository, prNumber, userID, userName)
	if errors.Is(err, store.ErrActiveReviewExists) {
		return reviewdomain.ErrActiveReviewExists
	}
	return err
}

// GetActive implements notificationdomain.ReviewSessions.
func (r *CodeReviewsRepo) GetActive(ctx context.Context, repository string, prNumber int) (notificationdomain.ReviewSession, error) {
	review, err := r.codeReviews.GetActive(ctx, repository, prNumber)
	if errors.Is(err, store.ErrNotFound) {
		return notificationdomain.ReviewSession{}, notificationdomain.ErrNoActiveReview
	}
	if err != nil {
		return notificationdomain.ReviewSession{}, err
	}
	return notificationdomain.ReviewSession{SlackUserID: review.SlackUserID, SlackUserName: review.SlackUserName}, nil
}

// Finish implements notificationdomain.ReviewSessions.
func (r *CodeReviewsRepo) Finish(ctx context.Context, repository string, prNumber int) error {
	return r.codeReviews.Finish(ctx, repository, prNumber)
}

// Reviewers implements notificationdomain.ReviewSessions.
func (r *CodeReviewsRepo) Reviewers(ctx context.Context, repository string, prNumber int) ([]notificationdomain.ReviewSession, error) {
	reviews, err := r.codeReviews.Reviewers(ctx, repository, prNumber)
	if err != nil {
		return nil, err
	}
	out := make([]notificationdomain.ReviewSession, len(reviews))
	for i, review := range reviews {
		out[i] = notificationdomain.ReviewSession{SlackUserID: review.SlackUserID, SlackUserName: review.SlackUserName}
	}
	return out, nil
}

var (
	_ reviewdomain.Recorder             = (*CodeReviewsRepo)(nil)
	_ notificationdomain.ReviewSessions = (*CodeReviewsRepo)(nil)
)
