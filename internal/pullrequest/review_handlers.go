package pullrequest

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/aireview"
	"github.com/mptooling/notifycat/internal/store"
)

// reactionHandler is the shared implementation behind the three review-state
// handlers (Approve, Commented, RequestChange). Each handler is just this
// struct + a different Applicable rule and an emojiOf function.
type reactionHandler struct {
	name       string // "approve" | "commented" | "request_change"
	emojiOf    func(store.Reactions) string
	applicable func(Event) bool

	store     Store
	behavior  RepoBehavior
	messenger Messenger
	logger    *slog.Logger
	detector  *aireview.Detector
	reviews   ReviewSessions
}

func approvedEmoji(r store.Reactions) string      { return r.Approved }
func commentedEmoji(r store.Reactions) string     { return r.Commented }
func requestChangeEmoji(r store.Reactions) string { return r.RequestChange }

func (h *reactionHandler) Applicable(e Event) bool { return h.applicable(e) }

func (h *reactionHandler) Handle(ctx context.Context, e Event) error {
	messages, err := h.store.Messages(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		h.logger.Info("ignored webhook event",
			slog.String("reason", "no_stored_message"),
			slog.String("handler", h.name),
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

	behavior, err := h.behavior.Get(ctx, e.Repository)
	if errors.Is(err, store.ErrNotFound) {
		h.logger.Warn("ignored webhook event",
			slog.String("reason", "no_mapping"),
			slog.String("handler", h.name),
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

	if behavior.IgnoreAIReviews && h.detector.IsBot(e.Sender.Type) {
		h.logger.Debug("skipped bot reviewer reaction",
			slog.String("login", e.Sender.Login),
			slog.String("event", e.GitHubEvent),
			slog.String("handler", h.name),
			slog.String("repository", e.Repository),
			slog.Int("pr", e.PR.Number),
		)
		return nil
	}

	emoji := h.emojiOf(behavior.Reactions)
	isBot := h.detector.IsBot(e.Sender.Type)
	// AddReaction is idempotent — the messenger treats an "already reacted"
	// response as success — so reacting on every stored message is safe to
	// replay. A surviving bot reviewer also gets a distinct marker per message
	// (empty BotReview turns the marker off).
	for _, m := range messages {
		if err := h.messenger.AddReaction(ctx, m.Channel, m.MessageID, emoji); err != nil {
			return err
		}
		if behavior.Reactions.BotReview != "" && isBot {
			if err := h.messenger.AddReaction(ctx, m.Channel, m.MessageID, behavior.Reactions.BotReview); err != nil {
				return err
			}
		}
	}

	// Record this review as activity so the stuck-PR digest stops nagging about
	// the PR until it next goes quiet. Suppressed bot reviews return above and
	// intentionally do not count — an ignored AI review is not human attention.
	if err := h.store.Touch(ctx, e.Repository, e.PR.Number); err != nil {
		return err
	}
	// A submitted GitHub review closes any active review session for the PR.
	// v1 has no GitHub-login↔Slack-user map, so a submission finishes every
	// active session on the PR (Finish is idempotent — no active session is a
	// no-op). Comment-only events (pull_request_review_comment, issue_comment)
	// and edited reviews are intentionally excluded by this gate.
	if e.GitHubEvent == "pull_request_review" && e.Action == "submitted" {
		if err := h.reviews.Finish(ctx, e.Repository, e.PR.Number); err != nil {
			return err
		}
	}
	return nil
}

// ApproveHandler adds a reaction when a review is submitted with state
// "approved".
type ApproveHandler struct{ reactionHandler }

// NewApproveHandler builds an ApproveHandler.
func NewApproveHandler(
	store Store,
	behavior RepoBehavior,
	messenger Messenger,
	logger *slog.Logger,
	detector *aireview.Detector,
	reviews ReviewSessions,
) *ApproveHandler {
	return &ApproveHandler{reactionHandler{
		name:    "approve",
		emojiOf: approvedEmoji,
		store:   store, behavior: behavior, messenger: messenger, logger: logger, detector: detector, reviews: reviews,
		applicable: func(e Event) bool {
			return e.Action == "submitted" && e.Review != nil && e.Review.State == "approved"
		},
	}}
}

// CommentedHandler adds a reaction when a review is submitted or edited with
// state "commented".
type CommentedHandler struct{ reactionHandler }

// NewCommentedHandler builds a CommentedHandler.
func NewCommentedHandler(
	store Store,
	behavior RepoBehavior,
	messenger Messenger,
	logger *slog.Logger,
	detector *aireview.Detector,
	reviews ReviewSessions,
) *CommentedHandler {
	return &CommentedHandler{reactionHandler{
		name:    "commented",
		emojiOf: commentedEmoji,
		store:   store, behavior: behavior, messenger: messenger, logger: logger, detector: detector, reviews: reviews,
		applicable: func(e Event) bool {
			if e.GitHubEvent == "pull_request_review_comment" {
				return e.Action == "created"
			}
			if e.GitHubEvent == "issue_comment" {
				return e.Action == "created" && e.PRComment
			}
			if e.Review == nil || e.Review.State != "commented" {
				return false
			}
			return e.Action == "submitted" || e.Action == "edited"
		},
	}}
}

// RequestChangeHandler adds a reaction when a review is submitted with state
// "changes_requested".
type RequestChangeHandler struct{ reactionHandler }

// NewRequestChangeHandler builds a RequestChangeHandler.
func NewRequestChangeHandler(
	store Store,
	behavior RepoBehavior,
	messenger Messenger,
	logger *slog.Logger,
	detector *aireview.Detector,
	reviews ReviewSessions,
) *RequestChangeHandler {
	return &RequestChangeHandler{reactionHandler{
		name:    "request_change",
		emojiOf: requestChangeEmoji,
		store:   store, behavior: behavior, messenger: messenger, logger: logger, detector: detector, reviews: reviews,
		applicable: func(e Event) bool {
			return e.Action == "submitted" && e.Review != nil && e.Review.State == "changes_requested"
		},
	}}
}
