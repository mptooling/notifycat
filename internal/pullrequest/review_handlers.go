package pullrequest

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/store"
)

// reactionHandler is the shared implementation behind the three review-state
// handlers (Approve, Commented, RequestChange). Each handler is just this
// struct + a different Applicable rule and a different emoji.
type reactionHandler struct {
	name       string // "approve" | "commented" | "request_change"
	emoji      string
	applicable func(Event) bool

	messages SlackMessages
	mappings RepoMappings
	slack    SlackClient
	logger   *slog.Logger
}

func (h *reactionHandler) Applicable(e Event) bool { return h.applicable(e) }

func (h *reactionHandler) Handle(ctx context.Context, e Event) error {
	stored, err := h.messages.Get(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		h.logger.Info("no stored message for review event",
			slog.String("handler", h.name),
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
		h.logger.Warn("no slack mapping for repository",
			slog.String("handler", h.name),
			slog.String("repository", e.Repository),
		)
		return nil
	}
	if err != nil {
		return err
	}

	// The slack.Client treats Slack's "already_reacted" error as a non-error,
	// so AddReaction is naturally idempotent. We don't need a GetReactions
	// pre-check (the PHP service did one, but it's redundant once the client
	// handles the duplicate-add case directly).
	return h.slack.AddReaction(ctx, mapping.SlackChannel, stored.TS, h.emoji)
}

// ApproveHandler adds a reaction when a review is submitted with state
// "approved".
type ApproveHandler struct{ reactionHandler }

// NewApproveHandler builds an ApproveHandler.
func NewApproveHandler(
	messages SlackMessages,
	mappings RepoMappings,
	slackClient SlackClient,
	logger *slog.Logger,
	emoji string,
) *ApproveHandler {
	return &ApproveHandler{reactionHandler{
		name:     "approve",
		emoji:    emoji,
		messages: messages, mappings: mappings, slack: slackClient, logger: logger,
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
	messages SlackMessages,
	mappings RepoMappings,
	slackClient SlackClient,
	logger *slog.Logger,
	emoji string,
) *CommentedHandler {
	return &CommentedHandler{reactionHandler{
		name:     "commented",
		emoji:    emoji,
		messages: messages, mappings: mappings, slack: slackClient, logger: logger,
		applicable: func(e Event) bool {
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
	messages SlackMessages,
	mappings RepoMappings,
	slackClient SlackClient,
	logger *slog.Logger,
	emoji string,
) *RequestChangeHandler {
	return &RequestChangeHandler{reactionHandler{
		name:     "request_change",
		emoji:    emoji,
		messages: messages, mappings: mappings, slack: slackClient, logger: logger,
		applicable: func(e Event) bool {
			return e.Action == "submitted" && e.Review != nil && e.Review.State == "changes_requested"
		},
	}}
}
