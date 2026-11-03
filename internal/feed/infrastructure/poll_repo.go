package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// PollRepo adapts persistence.FeedPolls to domain.PollStore.
type PollRepo struct {
	polls *persistence.FeedPolls
}

var _ domain.PollStore = (*PollRepo)(nil)

// NewPollRepo wraps the polls repository.
func NewPollRepo(polls *persistence.FeedPolls) *PollRepo {
	return &PollRepo{polls: polls}
}

// Record implements domain.PollStore.
func (repo *PollRepo) Record(ctx context.Context, tokenID string, polledAt time.Time) error {
	if err := repo.polls.Record(ctx, tokenID, polledAt); err != nil {
		return fmt.Errorf("feed: record poll: %w", err)
	}
	return nil
}
