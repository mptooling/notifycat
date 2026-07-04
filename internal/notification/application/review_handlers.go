package application

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// reactionHandler is the shared implementation behind the three review-state
// handlers (Approve, Commented, RequestChange). Each handler is just this struct
// plus a different Applicable rule and an emojiOf function.
type reactionHandler struct {
	name       string // "approve" | "commented" | "request_change"
	emojiOf    func(routingdomain.Reactions) string
	applicable func(kernel.Event) bool

	store     domain.MessageStore
	behavior  domain.RepoBehavior
	messenger domain.Messenger
	logger    *slog.Logger
	reviews   domain.ReviewSessions
}

func approvedEmoji(r routingdomain.Reactions) string      { return r.Approved }
func commentedEmoji(r routingdomain.Reactions) string     { return r.Commented }
func requestChangeEmoji(r routingdomain.Reactions) string { return r.RequestChange }

func (h *reactionHandler) Applicable(event kernel.Event) bool { return h.applicable(event) }

func (h *reactionHandler) Handle(ctx context.Context, event kernel.Event) error {
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

	if behavior.IgnoreAIReviews && domain.IsBot(event.Sender.Type) {
		h.logSkippedBotReviewer(event)
		return nil
	}

	if err := h.addReactions(ctx, event, behavior, messages); err != nil {
		return err
	}
	// Count the review as activity so the stuck-PR digest stops nagging until the
	// PR next goes quiet; suppressed bots returned above and do not count.
	if err := h.store.Touch(ctx, event.Repository, event.PR.Number); err != nil {
		return err
	}
	return h.finishSubmittedReview(ctx, event, behavior, messages)
}

// logIgnored records a silent no-op with its reason. no_mapping is an operator
// misconfiguration (warn); a missing stored message is a normal no-op (info).
func (h *reactionHandler) logIgnored(event kernel.Event, reason string) {
	attrs := []any{
		slog.String("reason", reason),
		slog.String("handler", h.name),
		slog.String("github_event", string(event.GitHubEvent)),
		slog.String("action", string(event.Action)),
		slog.String("repository", event.Repository),
		slog.Int("pr", event.PR.Number),
	}
	if reason == domain.ReasonNoMapping {
		h.logger.Warn("ignored webhook event", attrs...)
		return
	}
	h.logger.Info("ignored webhook event", attrs...)
}

// logSkippedBotReviewer records, at debug, that IgnoreAIReviews dropped a bot
// reviewer's reaction (and, via the early return, its activity credit).
func (h *reactionHandler) logSkippedBotReviewer(event kernel.Event) {
	h.logger.Debug("skipped bot reviewer reaction",
		slog.String("login", event.Sender.Login),
		slog.String("event", string(event.GitHubEvent)),
		slog.String("handler", h.name),
		slog.String("repository", event.Repository),
		slog.Int("pr", event.PR.Number),
	)
}

// addReactions applies the review's state emoji to every stored message, plus a
// distinct bot marker per message when a surviving bot reviewer is configured
// (empty BotReview turns the marker off). AddReaction is idempotent, so replaying
// it on every message is safe.
func (h *reactionHandler) addReactions(ctx context.Context, event kernel.Event, behavior routingdomain.RepoMapping, messages []domain.Message) error {
	emoji := h.emojiOf(behavior.Reactions)
	isBot := domain.IsBot(event.Sender.Type)
	for _, message := range messages {
		if err := h.messenger.AddReaction(ctx, message.Channel, message.MessageID, emoji); err != nil {
			return err
		}
		if behavior.Reactions.BotReview != "" && isBot {
			if err := h.messenger.AddReaction(ctx, message.Channel, message.MessageID, behavior.Reactions.BotReview); err != nil {
				return err
			}
		}
	}
	return nil
}

// finishSubmittedReview closes the PR's review sessions and takes the message out
// of the in-review state — but only on a true review submit; comment-only events
// and edited reviews are excluded. A submission finishes every active session at
// once (Finish is idempotent). Only when a session was active is the message
// rebuilt out of the in-review state.
func (h *reactionHandler) finishSubmittedReview(ctx context.Context, event kernel.Event, behavior routingdomain.RepoMapping, messages []domain.Message) error {
	if event.GitHubEvent != kernel.EventPullRequestReview || event.Action != kernel.ActionSubmitted {
		return nil
	}

	_, activeErr := h.reviews.GetActive(ctx, event.Repository, event.PR.Number)
	if activeErr != nil && !errors.Is(activeErr, domain.ErrNoActiveReview) {
		return activeErr
	}
	hadActiveSession := activeErr == nil

	if err := h.reviews.Finish(ctx, event.Repository, event.PR.Number); err != nil {
		return err
	}
	if !hadActiveSession {
		return nil
	}
	return h.clearInReviewState(ctx, event, behavior, messages)
}

// clearInReviewState rebuilds every stored message out of the in-review state (a
// fresh "please review" message, reviewing markers gone) plus a muted "reviewed
// by" line listing everyone who clicked Start review. A reviewer-lookup failure
// soft-degrades — the markers still clear, only the "reviewed by" line is dropped.
func (h *reactionHandler) clearInReviewState(ctx context.Context, event kernel.Event, behavior routingdomain.RepoMapping, messages []domain.Message) error {
	var reviewerIDs []string
	reviewers, err := h.reviews.Reviewers(ctx, event.Repository, event.PR.Number)
	if err != nil {
		h.logger.Warn("could not load reviewers after review submit",
			slog.String("repository", event.Repository), slog.Int("pr", event.PR.Number), slog.Any("err", err))
	} else {
		reviewerIDs = distinctReviewerIDs(reviewers)
	}
	request := domain.ReviewFinishedRequest{
		Repository:  event.Repository,
		PR:          event.PR,
		ReviewerIDs: reviewerIDs,
		NewPREmoji:  behavior.Reactions.NewPR,
	}
	for _, message := range messages {
		if err := h.messenger.UpdateReviewFinished(ctx, message.Channel, message.MessageID, request); err != nil {
			return err
		}
	}
	return nil
}

// ApproveHandler adds a reaction when a review is submitted with state "approved".
type ApproveHandler struct{ reactionHandler }

// NewApproveHandler builds an ApproveHandler.
func NewApproveHandler(store domain.MessageStore, behavior domain.RepoBehavior, messenger domain.Messenger, logger *slog.Logger, reviews domain.ReviewSessions) *ApproveHandler {
	return &ApproveHandler{reactionHandler{
		name:    "approve",
		emojiOf: approvedEmoji,
		store:   store, behavior: behavior, messenger: messenger, logger: logger, reviews: reviews,
		applicable: func(event kernel.Event) bool {
			return event.Action == kernel.ActionSubmitted && event.Review != nil && event.Review.State == kernel.ReviewApproved
		},
	}}
}

// CommentedHandler adds a reaction when a review is submitted or edited with
// state "commented".
type CommentedHandler struct{ reactionHandler }

// NewCommentedHandler builds a CommentedHandler.
func NewCommentedHandler(store domain.MessageStore, behavior domain.RepoBehavior, messenger domain.Messenger, logger *slog.Logger, reviews domain.ReviewSessions) *CommentedHandler {
	return &CommentedHandler{reactionHandler{
		name:    "commented",
		emojiOf: commentedEmoji,
		store:   store, behavior: behavior, messenger: messenger, logger: logger, reviews: reviews,
		applicable: func(event kernel.Event) bool {
			if event.GitHubEvent == kernel.EventPullRequestReviewComment {
				return event.Action == kernel.ActionCreated
			}
			if event.GitHubEvent == kernel.EventIssueComment {
				return event.Action == kernel.ActionCreated && event.PRComment
			}
			if event.Review == nil || event.Review.State != kernel.ReviewCommented {
				return false
			}
			return event.Action == kernel.ActionSubmitted || event.Action == kernel.ActionEdited
		},
	}}
}

// RequestChangeHandler adds a reaction when a review is submitted with state
// "changes_requested".
type RequestChangeHandler struct{ reactionHandler }

// NewRequestChangeHandler builds a RequestChangeHandler.
func NewRequestChangeHandler(store domain.MessageStore, behavior domain.RepoBehavior, messenger domain.Messenger, logger *slog.Logger, reviews domain.ReviewSessions) *RequestChangeHandler {
	return &RequestChangeHandler{reactionHandler{
		name:    "request_change",
		emojiOf: requestChangeEmoji,
		store:   store, behavior: behavior, messenger: messenger, logger: logger, reviews: reviews,
		applicable: func(event kernel.Event) bool {
			return event.Action == kernel.ActionSubmitted && event.Review != nil && event.Review.State == kernel.ReviewChangesRequested
		},
	}}
}

var (
	_ domain.Handler = (*ApproveHandler)(nil)
	_ domain.Handler = (*CommentedHandler)(nil)
	_ domain.Handler = (*RequestChangeHandler)(nil)
)
