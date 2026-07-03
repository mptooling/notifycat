package pullrequest

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/aireview"
	"github.com/mptooling/notifycat/internal/slack"
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
	composer  *slack.Composer
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

	if behavior.IgnoreAIReviews && h.detector.IsBot(e.Sender.Type) {
		h.logSkippedBotReviewer(e)
		return nil
	}

	if err := h.addReactions(ctx, e, behavior, messages); err != nil {
		return err
	}
	// Count the review as activity so the stuck-PR digest stops nagging until the
	// PR next goes quiet; suppressed bots returned above and do not count.
	if err := h.store.Touch(ctx, e.Repository, e.PR.Number); err != nil {
		return err
	}
	return h.finishSubmittedReview(ctx, e, behavior, messages)
}

// logIgnored records a silent no-op with its reason. no_mapping is an operator
// misconfiguration (warn); a missing stored message is a normal no-op (info) —
// matching CloseHandler's split.
func (h *reactionHandler) logIgnored(e Event, reason string) {
	attrs := []any{
		slog.String("reason", reason),
		slog.String("handler", h.name),
		slog.String("github_event", e.GitHubEvent),
		slog.String("action", e.Action),
		slog.String("repository", e.Repository),
		slog.Int("pr", e.PR.Number),
	}
	if reason == "no_mapping" {
		h.logger.Warn("ignored webhook event", attrs...)
		return
	}
	h.logger.Info("ignored webhook event", attrs...)
}

// logSkippedBotReviewer records, at debug, that IgnoreAIReviews dropped a bot
// reviewer's reaction (and, via the early return, its activity credit).
func (h *reactionHandler) logSkippedBotReviewer(e Event) {
	h.logger.Debug("skipped bot reviewer reaction",
		slog.String("login", e.Sender.Login),
		slog.String("event", e.GitHubEvent),
		slog.String("handler", h.name),
		slog.String("repository", e.Repository),
		slog.Int("pr", e.PR.Number),
	)
}

// addReactions applies the review's state emoji to every stored message, plus a
// distinct bot marker per message when a surviving bot reviewer is configured
// (empty BotReview turns the marker off). AddReaction is idempotent — the
// messenger treats an "already reacted" response as success — so replaying it on
// every message is safe.
func (h *reactionHandler) addReactions(ctx context.Context, e Event, behavior store.RepoMapping, messages []store.Message) error {
	emoji := h.emojiOf(behavior.Reactions)
	isBot := h.detector.IsBot(e.Sender.Type)
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
	return nil
}

// finishSubmittedReview closes the PR's review sessions and takes the message out
// of the in-review state — but only on a true review submit; comment-only events
// (pull_request_review_comment, issue_comment) and edited reviews are excluded.
// v1 has no GitHub-login↔Slack-user map, so a submission finishes every active
// session at once (Finish is idempotent — no active session is a no-op). Only
// when a session was active is the message rebuilt out of the in-review state.
func (h *reactionHandler) finishSubmittedReview(ctx context.Context, e Event, behavior store.RepoMapping, messages []store.Message) error {
	if e.GitHubEvent != "pull_request_review" || e.Action != "submitted" {
		return nil
	}

	_, activeErr := h.reviews.GetActive(ctx, e.Repository, e.PR.Number)
	if activeErr != nil && !errors.Is(activeErr, store.ErrNotFound) {
		return activeErr
	}
	hadActiveSession := activeErr == nil

	if err := h.reviews.Finish(ctx, e.Repository, e.PR.Number); err != nil {
		return err
	}
	if !hadActiveSession {
		return nil
	}
	return h.clearInReviewState(ctx, e, behavior, messages)
}

// clearInReviewState rebuilds every stored message out of the in-review state: a
// fresh "please review" message (":eye: reviewing" markers gone, "Start review"
// button kept so the still-open PR can be picked up again) plus a muted
// "reviewed by" line listing everyone who clicked Start review. A reviewer-lookup
// failure soft-degrades — the markers still clear, only the "reviewed by" line is
// dropped — rather than leaving the message stale.
func (h *reactionHandler) clearInReviewState(ctx context.Context, e Event, behavior store.RepoMapping, messages []store.Message) error {
	updated := h.composer.NewMessage(slackPRFrom(e), nil, behavior.Reactions.NewPR)
	reviewers, err := h.reviews.Reviewers(ctx, e.Repository, e.PR.Number)
	if err != nil {
		h.logger.Warn("could not load reviewers after review submit",
			slog.String("repository", e.Repository), slog.Int("pr", e.PR.Number), slog.Any("err", err))
	} else if userIDs := distinctReviewerIDs(reviewers); len(userIDs) > 0 {
		updated.Blocks = append(updated.Blocks, h.composer.ReviewedByMarker(userIDs))
	}
	for _, m := range messages {
		if err := h.messenger.UpdateMessage(ctx, m.Channel, m.MessageID, updated); err != nil {
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
	composer *slack.Composer,
	logger *slog.Logger,
	detector *aireview.Detector,
	reviews ReviewSessions,
) *ApproveHandler {
	return &ApproveHandler{reactionHandler{
		name:    "approve",
		emojiOf: approvedEmoji,
		store:   store, behavior: behavior, messenger: messenger, composer: composer, logger: logger, detector: detector, reviews: reviews,
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
	composer *slack.Composer,
	logger *slog.Logger,
	detector *aireview.Detector,
	reviews ReviewSessions,
) *CommentedHandler {
	return &CommentedHandler{reactionHandler{
		name:    "commented",
		emojiOf: commentedEmoji,
		store:   store, behavior: behavior, messenger: messenger, composer: composer, logger: logger, detector: detector, reviews: reviews,
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
	composer *slack.Composer,
	logger *slog.Logger,
	detector *aireview.Detector,
	reviews ReviewSessions,
) *RequestChangeHandler {
	return &RequestChangeHandler{reactionHandler{
		name:    "request_change",
		emojiOf: requestChangeEmoji,
		store:   store, behavior: behavior, messenger: messenger, composer: composer, logger: logger, detector: detector, reviews: reviews,
		applicable: func(e Event) bool {
			return e.Action == "submitted" && e.Review != nil && e.Review.State == "changes_requested"
		},
	}}
}
