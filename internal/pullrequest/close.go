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
	store     Store
	behavior  RepoBehavior
	messenger Messenger
	composer  *slack.Composer
	logger    *slog.Logger
	reviews   ReviewSessions
}

// NewCloseHandler builds a CloseHandler.
func NewCloseHandler(
	store Store,
	behavior RepoBehavior,
	messenger Messenger,
	composer *slack.Composer,
	logger *slog.Logger,
	reviews ReviewSessions,
) *CloseHandler {
	return &CloseHandler{
		store:     store,
		behavior:  behavior,
		messenger: messenger,
		composer:  composer,
		logger:    logger,
		reviews:   reviews,
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

	reviewers, err := h.reviews.Reviewers(ctx, e.Repository, e.PR.Number)
	if err != nil {
		// Supplementary to the close decoration — log and proceed without it
		// rather than dropping the Merged/Closed update.
		h.logger.Warn("could not load reviewers for closed PR",
			slog.String("repository", e.Repository), slog.Int("pr", e.PR.Number), slog.Any("err", err))
		reviewers = nil
	}
	if userIDs := distinctReviewerIDs(reviewers); len(userIDs) > 0 {
		updated.Blocks = append(updated.Blocks, h.composer.ReviewedByMarker(userIDs))
	}

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
	if err := h.reviews.Finish(ctx, e.Repository, e.PR.Number); err != nil {
		return err
	}
	return h.store.MarkClosed(ctx, e.Repository, e.PR.Number)
}

// distinctReviewerIDs returns the reviewers' Slack user IDs in first-seen order,
// deduped — a user with several sessions on the PR is listed once.
func distinctReviewerIDs(reviews []store.CodeReview) []string {
	seen := map[string]bool{}
	var ids []string
	for _, review := range reviews {
		if review.SlackUserID == "" || seen[review.SlackUserID] {
			continue
		}
		seen[review.SlackUserID] = true
		ids = append(ids, review.SlackUserID)
	}
	return ids
}

func (h *CloseHandler) logIgnored(e Event, reason string) {
	attrs := []any{
		slog.String("reason", reason),
		slog.String("handler", "close"),
		slog.String("github_event", e.GitHubEvent),
		slog.String("action", e.Action),
		slog.String("repository", e.Repository),
		slog.Int("pr", e.PR.Number),
	}
	// no_mapping is an operator misconfiguration (warn); a missing stored
	// message is a normal no-op (info), matching the pre-fan-out behavior.
	if reason == "no_mapping" {
		h.logger.Warn("ignored webhook event", attrs...)
		return
	}
	h.logger.Info("ignored webhook event", attrs...)
}
