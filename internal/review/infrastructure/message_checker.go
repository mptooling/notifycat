package infrastructure

import (
	"context"
	"errors"

	"github.com/mptooling/notifycat/internal/platform/persistence"
	reviewdomain "github.com/mptooling/notifycat/internal/review/domain"
)

// MessageChecker adapts the store's PullRequests repository to the review
// MessageChecker port: an untracked PR reports false rather than erroring.
type MessageChecker struct {
	pullRequests *persistence.PullRequests
}

// NewMessageChecker wraps the store's PullRequests repository.
func NewMessageChecker(pullRequests *persistence.PullRequests) *MessageChecker {
	return &MessageChecker{pullRequests: pullRequests}
}

// HasMessages implements reviewdomain.MessageChecker.
func (c *MessageChecker) HasMessages(ctx context.Context, repository string, prNumber int) (bool, error) {
	messages, err := c.pullRequests.Messages(ctx, repository, prNumber)
	if errors.Is(err, persistence.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(messages) > 0, nil
}

var _ reviewdomain.MessageChecker = (*MessageChecker)(nil)
