package infrastructure

import (
	"context"
	"time"

	"github.com/mptooling/notifycat/internal/digest/domain"
	"github.com/mptooling/notifycat/internal/store"
)

// StuckRepo adapts the store's PullRequests repository to the digest's
// StuckFinder port, mapping store persistence models to digest domain DTOs at
// the boundary so no gorm-tagged type crosses a port.
type StuckRepo struct {
	pullRequests *store.PullRequests
}

// NewStuckRepo wraps the store's PullRequests repository.
func NewStuckRepo(pullRequests *store.PullRequests) *StuckRepo {
	return &StuckRepo{pullRequests: pullRequests}
}

// FindStuck implements domain.StuckFinder, mapping store rows (messages
// preloaded) to digest domain PullRequests.
func (r *StuckRepo) FindStuck(ctx context.Context, cutoff time.Time) ([]domain.PullRequest, error) {
	rows, err := r.pullRequests.FindStuck(ctx, cutoff)
	if err != nil {
		return nil, err
	}
	out := make([]domain.PullRequest, len(rows))
	for i, row := range rows {
		messages := make([]domain.MessageRef, len(row.Messages))
		for j, message := range row.Messages {
			messages[j] = domain.MessageRef{Channel: message.Channel, MessageID: message.MessageID}
		}
		out[i] = domain.PullRequest{
			Repository: row.Repository,
			PRNumber:   row.PRNumber,
			UpdatedAt:  row.UpdatedAt,
			Messages:   messages,
		}
	}
	return out, nil
}

var _ domain.StuckFinder = (*StuckRepo)(nil)
