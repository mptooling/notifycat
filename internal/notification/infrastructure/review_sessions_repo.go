package infrastructure

import (
	"context"
	"errors"

	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/store"
)

// ReviewSessionsRepo adapts the store's CodeReviews repository to the
// notification ReviewSessions port, mapping store models to domain DTOs and the
// store's not-found sentinel to ErrNoActiveReview. It is a transition adapter:
// the review domain owns this port in a later phase.
type ReviewSessionsRepo struct {
	codeReviews *store.CodeReviews
}

// NewReviewSessionsRepo wraps the store's CodeReviews repository.
func NewReviewSessionsRepo(codeReviews *store.CodeReviews) *ReviewSessionsRepo {
	return &ReviewSessionsRepo{codeReviews: codeReviews}
}

// GetActive implements domain.ReviewSessions, translating the store's not-found
// sentinel to domain.ErrNoActiveReview.
func (r *ReviewSessionsRepo) GetActive(ctx context.Context, repository string, prNumber int) (domain.ReviewSession, error) {
	review, err := r.codeReviews.GetActive(ctx, repository, prNumber)
	if errors.Is(err, store.ErrNotFound) {
		return domain.ReviewSession{}, domain.ErrNoActiveReview
	}
	if err != nil {
		return domain.ReviewSession{}, err
	}
	return domain.ReviewSession{SlackUserID: review.SlackUserID, SlackUserName: review.SlackUserName}, nil
}

// Finish implements domain.ReviewSessions.
func (r *ReviewSessionsRepo) Finish(ctx context.Context, repository string, prNumber int) error {
	return r.codeReviews.Finish(ctx, repository, prNumber)
}

// Reviewers implements domain.ReviewSessions, mapping store rows to domain DTOs.
func (r *ReviewSessionsRepo) Reviewers(ctx context.Context, repository string, prNumber int) ([]domain.ReviewSession, error) {
	reviews, err := r.codeReviews.Reviewers(ctx, repository, prNumber)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ReviewSession, len(reviews))
	for i, review := range reviews {
		out[i] = domain.ReviewSession{SlackUserID: review.SlackUserID, SlackUserName: review.SlackUserName}
	}
	return out, nil
}

var _ domain.ReviewSessions = (*ReviewSessionsRepo)(nil)
