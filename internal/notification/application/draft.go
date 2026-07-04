package application

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// DraftHandler reacts to a PR being converted back to draft. It removes every
// stored message and deletes the PR row — the PR will be re-announced when it is
// marked ready_for_review again.
type DraftHandler struct {
	store     domain.MessageStore
	messenger domain.Messenger
	logger    *slog.Logger
}

// NewDraftHandler builds a DraftHandler.
func NewDraftHandler(store domain.MessageStore, messenger domain.Messenger, logger *slog.Logger) *DraftHandler {
	return &DraftHandler{store: store, messenger: messenger, logger: logger}
}

// Applicable returns true when the action is "converted_to_draft".
func (h *DraftHandler) Applicable(event kernel.Event) bool {
	return event.Action == kernel.ActionConvertedToDraft
}

// Handle deletes every stored message and the PR row.
func (h *DraftHandler) Handle(ctx context.Context, event kernel.Event) error {
	messages, err := h.store.Messages(ctx, event.Repository, event.PR.Number)
	if errors.Is(err, routingdomain.ErrNotFound) {
		h.logger.Info("ignored webhook event",
			slog.String("reason", domain.ReasonNoStoredMessage),
			slog.String("handler", "draft"),
			slog.String("github_event", string(event.GitHubEvent)),
			slog.String("action", string(event.Action)),
			slog.String("repository", event.Repository),
			slog.Int("pr", event.PR.Number),
		)
		return nil
	}
	if err != nil {
		return err
	}

	for _, message := range messages {
		if err := h.messenger.Delete(ctx, message.Channel, message.MessageID); err != nil {
			return err
		}
	}
	return h.store.Delete(ctx, event.Repository, event.PR.Number)
}

var _ domain.Handler = (*DraftHandler)(nil)
