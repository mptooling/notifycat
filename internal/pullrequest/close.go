package pullrequest

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// CloseOptions tunes the CloseHandler. Reactions on close are toggleable
// because that's how the legacy PHP service exposed them.
type CloseOptions struct {
	ReactionsEnabled bool
	MergedEmoji      string
	ClosedEmoji      string
}

// CloseHandler reacts to a PR being closed (merged or not). It updates the
// original Slack message with a [Merged]/[Closed] decoration and, if enabled,
// adds the corresponding reaction emoji.
type CloseHandler struct {
	messages SlackMessages
	mappings RepoMappings
	slack    SlackClient
	composer *slack.Composer
	logger   *slog.Logger
	opts     CloseOptions
}

// NewCloseHandler builds a CloseHandler.
func NewCloseHandler(
	messages SlackMessages,
	mappings RepoMappings,
	slackClient SlackClient,
	composer *slack.Composer,
	logger *slog.Logger,
	opts CloseOptions,
) *CloseHandler {
	return &CloseHandler{
		messages: messages,
		mappings: mappings,
		slack:    slackClient,
		composer: composer,
		logger:   logger,
		opts:     opts,
	}
}

// Applicable returns true when the action is "closed".
func (h *CloseHandler) Applicable(e Event) bool { return e.Action == "closed" }

// Handle updates the stored Slack message and optionally adds a reaction.
func (h *CloseHandler) Handle(ctx context.Context, e Event) error {
	stored, err := h.messages.Get(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		h.logger.Info("no stored message for closed PR",
			slog.String("repository", e.Repository),
			slog.Int("pr", e.PR.Number),
		)
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

	original := h.composer.NewMessage(slackPRFrom(e), mapping.Mentions)
	updated := h.composer.UpdatedMessage(e.PR.Merged, original)

	if err := h.slack.UpdateMessage(ctx, mapping.SlackChannel, stored.TS, updated); err != nil {
		return err
	}
	if !h.opts.ReactionsEnabled {
		return nil
	}

	emoji := h.opts.ClosedEmoji
	if e.PR.Merged {
		emoji = h.opts.MergedEmoji
	}
	return h.slack.AddReaction(ctx, mapping.SlackChannel, stored.TS, emoji)
}
