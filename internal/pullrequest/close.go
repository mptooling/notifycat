package pullrequest

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// CloseHandler reacts to a PR being closed (merged or not). It updates the
// original Slack message with a [Merged]/[Closed] decoration and, if enabled,
// adds the corresponding reaction emoji.
type CloseHandler struct {
	messages SlackMessages
	mappings RepoMappings
	slack    SlackClient
	composer *slack.Composer
	logger   *slog.Logger
}

// NewCloseHandler builds a CloseHandler.
func NewCloseHandler(
	messages SlackMessages,
	mappings RepoMappings,
	slackClient SlackClient,
	composer *slack.Composer,
	logger *slog.Logger,
) *CloseHandler {
	return &CloseHandler{
		messages: messages,
		mappings: mappings,
		slack:    slackClient,
		composer: composer,
		logger:   logger,
	}
}

// Applicable returns true when the action is "closed".
func (h *CloseHandler) Applicable(e Event) bool { return e.Action == "closed" }

// Handle updates the stored Slack message and optionally adds a reaction.
func (h *CloseHandler) Handle(ctx context.Context, e Event) error {
	stored, err := h.messages.Get(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		h.logger.Info("ignored webhook event",
			slog.String("reason", "no_stored_message"),
			slog.String("handler", "close"),
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

	mapping, err := h.mappings.Get(ctx, e.Repository)
	if errors.Is(err, store.ErrNotFound) {
		h.logger.Warn("ignored webhook event",
			slog.String("reason", "no_mapping"),
			slog.String("handler", "close"),
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

	emoji := mapping.Reactions.ClosedPR
	if e.PR.Merged {
		emoji = mapping.Reactions.MergedPR
	}
	updated := h.composer.UpdatedMessage(slackPRFrom(e), e.PR.Merged, emoji)
	if err := h.slack.UpdateMessage(ctx, mapping.SlackChannel, stored.TS, updated); err != nil {
		return err
	}
	if err := h.messages.MarkClosed(ctx, e.Repository, e.PR.Number); err != nil {
		return err
	}
	if !mapping.Reactions.Enabled {
		return nil
	}
	return h.slack.AddReaction(ctx, mapping.SlackChannel, stored.TS, emoji)
}
