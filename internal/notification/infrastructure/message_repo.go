package infrastructure

import (
	"context"

	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/store"
)

// MessageRepo adapts the store's PullRequests repository to the notification
// MessageStore port, mapping store persistence models to domain DTOs at the
// boundary so no gorm-tagged type crosses a port. The "not found" sentinel
// passes through unchanged (store.ErrNotFound aliases the routing domain
// sentinel handlers compare against).
type MessageRepo struct {
	pullRequests *store.PullRequests
}

// NewMessageRepo wraps the store's PullRequests repository.
func NewMessageRepo(pullRequests *store.PullRequests) *MessageRepo {
	return &MessageRepo{pullRequests: pullRequests}
}

// AddMessage implements domain.MessageStore.
func (r *MessageRepo) AddMessage(ctx context.Context, repository string, prNumber int, channel, messageID string) error {
	return r.pullRequests.AddMessage(ctx, repository, prNumber, channel, messageID)
}

// Messages implements domain.MessageStore, mapping store rows to domain Messages.
func (r *MessageRepo) Messages(ctx context.Context, repository string, prNumber int) ([]domain.Message, error) {
	rows, err := r.pullRequests.Messages(ctx, repository, prNumber)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Message, len(rows))
	for i, message := range rows {
		out[i] = domain.Message{Channel: message.Channel, MessageID: message.MessageID}
	}
	return out, nil
}

// Touch implements domain.MessageStore.
func (r *MessageRepo) Touch(ctx context.Context, repository string, prNumber int) error {
	return r.pullRequests.Touch(ctx, repository, prNumber)
}

// MarkClosed implements domain.MessageStore.
func (r *MessageRepo) MarkClosed(ctx context.Context, repository string, prNumber int) error {
	return r.pullRequests.MarkClosed(ctx, repository, prNumber)
}

// Delete implements domain.MessageStore.
func (r *MessageRepo) Delete(ctx context.Context, repository string, prNumber int) error {
	return r.pullRequests.Delete(ctx, repository, prNumber)
}

var _ domain.MessageStore = (*MessageRepo)(nil)
