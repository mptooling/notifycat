package application

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// CloseHandler reacts to a PR being closed (merged or not). It updates every
// stored message with a [Merged]/[Closed] decoration and, if enabled, adds the
// corresponding reaction to each.
type CloseHandler struct {
	store     domain.MessageStore
	behavior  domain.RepoBehavior
	messenger domain.Messenger
	logger    *slog.Logger
	reviews   domain.ReviewSessions
}

// NewCloseHandler builds a CloseHandler.
func NewCloseHandler(store domain.MessageStore, behavior domain.RepoBehavior, messenger domain.Messenger, logger *slog.Logger, reviews domain.ReviewSessions) *CloseHandler {
	return &CloseHandler{store: store, behavior: behavior, messenger: messenger, logger: logger, reviews: reviews}
}

// Applicable returns true when the action is "closed".
func (h *CloseHandler) Applicable(event kernel.Event) bool {
	return event.Action == kernel.ActionClosed
}

// Handle updates every stored message and optionally adds a reaction to each.
func (h *CloseHandler) Handle(ctx context.Context, event kernel.Event) error {
	messages, err := h.store.Messages(ctx, event.Repository, event.PR.Number)
	if errors.Is(err, routingdomain.ErrNotFound) {
		h.logIgnored(event, domain.ReasonNoStoredMessage)
		return nil
	}
	if err != nil {
		return err
	}

	behavior, err := h.behavior.Get(ctx, event.Repository)
	if errors.Is(err, routingdomain.ErrNotFound) {
		h.logIgnored(event, domain.ReasonNoMapping)
		return nil
	}
	if err != nil {
		return err
	}

	emoji := behavior.Reactions.ClosedPR
	if event.PR.Merged {
		emoji = behavior.Reactions.MergedPR
	}

	reviewers, err := h.reviews.Reviewers(ctx, event.Repository, event.PR.Number)
	if err != nil {
		// Supplementary to the close decoration — log and proceed without it
		// rather than dropping the Merged/Closed update.
		h.logger.Warn("could not load reviewers for closed PR",
			slog.String("repository", event.Repository), slog.Int("pr", event.PR.Number), slog.Any("err", err))
		reviewers = nil
	}

	request := domain.ClosedRequest{
		Repository:  event.Repository,
		PR:          event.PR,
		Merged:      event.PR.Merged,
		Emoji:       emoji,
		ReviewerIDs: distinctReviewerIDs(reviewers),
	}
	for _, message := range messages {
		if err := h.messenger.UpdateClosed(ctx, message.Channel, message.MessageID, request); err != nil {
			return err
		}
		if behavior.Reactions.Enabled {
			if err := h.messenger.AddReaction(ctx, message.Channel, message.MessageID, emoji); err != nil {
				return err
			}
		}
	}
	if err := h.reviews.Finish(ctx, event.Repository, event.PR.Number); err != nil {
		return err
	}
	return h.store.MarkClosed(ctx, event.Repository, event.PR.Number)
}

// distinctReviewerIDs returns the reviewers' Slack user IDs in first-seen order,
// deduped — a user with several sessions on the PR is listed once.
func distinctReviewerIDs(reviews []domain.ReviewSession) []string {
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

func (h *CloseHandler) logIgnored(event kernel.Event, reason string) {
	attrs := []any{
		slog.String("reason", reason),
		slog.String("handler", "close"),
		slog.String("github_event", string(event.GitHubEvent)),
		slog.String("action", string(event.Action)),
		slog.String("repository", event.Repository),
		slog.Int("pr", event.PR.Number),
	}
	// no_mapping is an operator misconfiguration (warn); a missing stored message
	// is a normal no-op (info), matching the pre-fan-out behavior.
	if reason == domain.ReasonNoMapping {
		h.logger.Warn("ignored webhook event", attrs...)
		return
	}
	h.logger.Info("ignored webhook event", attrs...)
}

var _ domain.Handler = (*CloseHandler)(nil)
