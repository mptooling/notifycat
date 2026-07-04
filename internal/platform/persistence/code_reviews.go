package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ErrActiveReviewExists is returned by Start when the same user already has an
// active review on the PR — the partial unique index rejected the insert.
// Callers surface the conflict UX ("already reviewing") instead of a 500.
var ErrActiveReviewExists = errors.New("store: active code review exists")

// CodeReviews persists per-PR review sessions and enforces at most one active
// review per (PR, user); multiple distinct users may review a PR concurrently.
// A review is identified to callers by its PR's natural key (repository,
// prNumber); the surrogate pull_request_id is resolved internally.
type CodeReviews struct {
	db *gorm.DB
}

// NewCodeReviews constructs a CodeReviews repository bound to db.
func NewCodeReviews(db *gorm.DB) *CodeReviews {
	return &CodeReviews{db: db}
}

// Start opens a review on the PR for the given Slack user. It returns
// ErrNotFound when the PR is not tracked, and ErrActiveReviewExists when the PR
// already has an active review — the DB's partial unique index is the source of
// truth, so two near-simultaneous Starts can't both win.
func (r *CodeReviews) Start(ctx context.Context, repository string, prNumber int, slackUserID, slackUserName string) error {
	prID, err := r.pullRequestID(ctx, repository, prNumber)
	if err != nil {
		return err
	}
	review := CodeReview{
		PullRequestID: prID,
		SlackUserID:   slackUserID,
		SlackUserName: slackUserName,
		StartedAt:     time.Now(),
	}
	if err := r.db.WithContext(ctx).Create(&review).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrActiveReviewExists
		}
		return fmt.Errorf("store: start code review: %w", err)
	}
	return nil
}

// GetActive returns the PR's active (unfinished) review, or ErrNotFound when the
// PR is untracked or has no review in progress.
func (r *CodeReviews) GetActive(ctx context.Context, repository string, prNumber int) (CodeReview, error) {
	var review CodeReview
	err := r.db.WithContext(ctx).
		Joins("JOIN pull_requests ON pull_requests.id = code_reviews.pull_request_id").
		Where("pull_requests.gh_repository = ? AND pull_requests.pr_number = ? AND code_reviews.finished_at IS NULL",
			repository, prNumber).
		First(&review).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CodeReview{}, ErrNotFound
	}
	if err != nil {
		return CodeReview{}, fmt.Errorf("store: get active code review: %w", err)
	}
	return review, nil
}

// ActiveForUser returns the user's active (unfinished) review on the PR, or
// ErrNotFound when that user has no review in progress here. It is the
// app-level guard the click handler checks before Start; the DB's partial
// unique index on (pull_request_id, slack_user_id) is the race-safe backstop.
func (r *CodeReviews) ActiveForUser(ctx context.Context, repository string, prNumber int, slackUserID string) (CodeReview, error) {
	var review CodeReview
	err := r.db.WithContext(ctx).
		Joins("JOIN pull_requests ON pull_requests.id = code_reviews.pull_request_id").
		Where("pull_requests.gh_repository = ? AND pull_requests.pr_number = ? AND code_reviews.slack_user_id = ? AND code_reviews.finished_at IS NULL",
			repository, prNumber, slackUserID).
		First(&review).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CodeReview{}, ErrNotFound
	}
	if err != nil {
		return CodeReview{}, fmt.Errorf("store: active code review for user: %w", err)
	}
	return review, nil
}

// Finish marks the PR's active review finished. It is idempotent: no active
// review (or an untracked PR) is a no-op, mirroring MarkClosed.
func (r *CodeReviews) Finish(ctx context.Context, repository string, prNumber int) error {
	res := r.db.WithContext(ctx).Model(&CodeReview{}).
		Where("finished_at IS NULL AND pull_request_id = (SELECT id FROM pull_requests WHERE gh_repository = ? AND pr_number = ?)",
			repository, prNumber).
		UpdateColumn("finished_at", time.Now())
	if res.Error != nil {
		return fmt.Errorf("store: finish code review: %w", res.Error)
	}
	return nil
}

// Reviewers returns all code-review sessions for the PR ordered by started_at
// ascending (earliest first). An untracked PR or a PR with no reviews returns
// an empty slice and nil error.
func (r *CodeReviews) Reviewers(ctx context.Context, repository string, prNumber int) ([]CodeReview, error) {
	var reviews []CodeReview
	err := r.db.WithContext(ctx).
		Joins("JOIN pull_requests ON pull_requests.id = code_reviews.pull_request_id").
		Where("pull_requests.gh_repository = ? AND pull_requests.pr_number = ?", repository, prNumber).
		Order("code_reviews.started_at ASC").
		Find(&reviews).Error
	if err != nil {
		return nil, fmt.Errorf("store: list reviewers: %w", err)
	}
	return reviews, nil
}

// pullRequestID resolves the surrogate id for (repository, prNumber), returning
// ErrNotFound when the PR is not tracked.
func (r *CodeReviews) pullRequestID(ctx context.Context, repository string, prNumber int) (uint, error) {
	var pr PullRequest
	err := r.db.WithContext(ctx).
		Where("gh_repository = ? AND pr_number = ?", repository, prNumber).
		First(&pr).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("store: load pull request: %w", err)
	}
	return pr.ID, nil
}

// isUniqueViolation reports whether err is a SQLite UNIQUE-constraint failure.
// db.go does not enable GORM's TranslateError and the pure-Go sqlite driver may
// not map to gorm.ErrDuplicatedKey, so we also match the driver message.
func isUniqueViolation(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey) ||
		strings.Contains(err.Error(), "UNIQUE constraint failed")
}
