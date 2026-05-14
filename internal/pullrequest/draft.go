package pullrequest

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/store"
)

// DraftHandler reacts to a PR being converted back to draft. It removes the
// Slack notification and forgets the message TS — the PR will be re-announced
// when it's marked ready_for_review again.
type DraftHandler struct {
	messages SlackMessages
	mappings RepoMappings
	slack    SlackClient
	logger   *slog.Logger
}

// NewDraftHandler builds a DraftHandler.
func NewDraftHandler(
	messages SlackMessages,
	mappings RepoMappings,
	slackClient SlackClient,
	logger *slog.Logger,
) *DraftHandler {
	return &DraftHandler{
		messages: messages,
		mappings: mappings,
		slack:    slackClient,
		logger:   logger,
	}
}

// Applicable returns true when the action is "converted_to_draft".
func (h *DraftHandler) Applicable(e Event) bool { return e.Action == "converted_to_draft" }

// Handle deletes the Slack message and the stored message row.
func (h *DraftHandler) Handle(ctx context.Context, e Event) error {
	stored, err := h.messages.Get(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	mapping, err := h.mappings.Get(ctx, e.Repository)
	if errors.Is(err, store.ErrNotFound) {
		h.logger.Warn("no slack mapping for repository", slog.String("repository", e.Repository))
		return nil
	}
	if err != nil {
		return err
	}

	if err := h.slack.DeleteMessage(ctx, mapping.SlackChannel, stored.TS); err != nil {
		return err
	}
	return h.messages.Delete(ctx, e.Repository, e.PR.Number)
}
