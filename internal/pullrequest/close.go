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

	// The merged/closed reaction emoji doubles as the message's leading emoji,
	// so it is selected regardless of whether reactions are also added.
	emoji := h.opts.ClosedEmoji
	if e.PR.Merged {
		emoji = h.opts.MergedEmoji
	}

	updated := h.composer.UpdatedMessage(slackPRFrom(e), e.PR.Merged, emoji)
	if err := h.slack.UpdateMessage(ctx, mapping.SlackChannel, stored.TS, updated); err != nil {
		return err
	}

	// Drop the PR from the stuck-PR digest now that it's merged/closed. The row
	// itself stays until the cleanup TTL; closed_at just hides it from reminders.
	if err := h.messages.MarkClosed(ctx, e.Repository, e.PR.Number); err != nil {
		return err
	}

	if !h.opts.ReactionsEnabled {
		return nil
	}
	return h.slack.AddReaction(ctx, mapping.SlackChannel, stored.TS, emoji)
}
