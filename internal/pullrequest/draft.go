package pullrequest

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/store"
)

// DraftHandler reacts to a PR being converted back to draft. It removes every
// stored Slack message and deletes the PR row — the PR will be re-announced
// when it's marked ready_for_review again.
type DraftHandler struct {
	store     Store
	messenger Messenger
	logger    *slog.Logger
}

// NewDraftHandler builds a DraftHandler.
func NewDraftHandler(store Store, messenger Messenger, logger *slog.Logger) *DraftHandler {
	return &DraftHandler{store: store, messenger: messenger, logger: logger}
}

// Applicable returns true when the action is "converted_to_draft".
func (h *DraftHandler) Applicable(e Event) bool { return e.Action == "converted_to_draft" }

// Handle deletes every stored Slack message and the PR row.
func (h *DraftHandler) Handle(ctx context.Context, e Event) error {
	messages, err := h.store.Messages(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		h.logger.Info("ignored webhook event",
			slog.String("reason", "no_stored_message"),
			slog.String("handler", "draft"),
			slog.String("github_event", e.GitHubEvent),
			slog.String("action", e.Action),
			slog.String("repository", e.Repository),
			slog.Int("pr", e.PR.Number),
		)
		return nil
	}
	if err != nil {
		return err
	}

	for _, m := range messages {
		if err := h.messenger.DeleteMessage(ctx, m.Channel, m.MessageID); err != nil {
			return err
		}
	}
	return h.store.Delete(ctx, e.Repository, e.PR.Number)
}
