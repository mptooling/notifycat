package pullrequest

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// CloseHandler reacts to a PR being closed (merged or not). It updates every
// stored Slack message with a [Merged]/[Closed] decoration and, if enabled,
// adds the corresponding reaction emoji to each one.
type CloseHandler struct {
	store     PullRequestStore
	behavior  RepoBehavior
	messenger Messenger
	composer  *slack.Composer
	logger    *slog.Logger
}

// NewCloseHandler builds a CloseHandler.
func NewCloseHandler(
	store PullRequestStore,
	behavior RepoBehavior,
	slackClient Messenger,
	composer *slack.Composer,
	logger *slog.Logger,
) *CloseHandler {
	return &CloseHandler{
		store:     store,
		behavior:  behavior,
		messenger: slackClient,
		composer:  composer,
		logger:    logger,
	}
}

// Applicable returns true when the action is "closed".
func (h *CloseHandler) Applicable(e Event) bool { return e.Action == "closed" }

// Handle updates every stored Slack message and optionally adds a reaction to each.
func (h *CloseHandler) Handle(ctx context.Context, e Event) error {
	messages, err := h.store.Messages(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		h.logIgnored(e, "no_stored_message")
		return nil
	}
	if err != nil {
		return err
	}

	behavior, err := h.behavior.Get(ctx, e.Repository)
	if errors.Is(err, store.ErrNotFound) {
		h.logIgnored(e, "no_mapping")
		return nil
	}
	if err != nil {
		return err
	}

	emoji := behavior.Reactions.ClosedPR
	if e.PR.Merged {
		emoji = behavior.Reactions.MergedPR
	}
	updated := h.composer.UpdatedMessage(slackPRFrom(e), e.PR.Merged, emoji)
	for _, m := range messages {
		if err := h.messenger.UpdateMessage(ctx, m.Channel, m.MessageID, updated); err != nil {
			return err
		}
		if behavior.Reactions.Enabled {
			if err := h.messenger.AddReaction(ctx, m.Channel, m.MessageID, emoji); err != nil {
				return err
			}
		}
	}
	return h.store.MarkClosed(ctx, e.Repository, e.PR.Number)
}

func (h *CloseHandler) logIgnored(e Event, reason string) {
	h.logger.Warn("ignored webhook event",
		slog.String("reason", reason),
		slog.String("handler", "close"),
		slog.String("github_event", e.GitHubEvent),
		slog.String("action", e.Action),
		slog.String("repository", e.Repository),
		slog.Int("pr", e.PR.Number),
	)
}
