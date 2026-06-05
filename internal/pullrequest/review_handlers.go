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
// struct + a different Applicable rule and a different emoji.
type reactionHandler struct {
	name       string // "approve" | "commented" | "request_change"
	emoji      string
	botEmoji   string // distinct marker for non-suppressed bot reviewers; "" disables it
	applicable func(Event) bool

	messages SlackMessages
	mappings RepoMappings
	slack    SlackClient
	logger   *slog.Logger
	detector *aireview.Detector
}

func (h *reactionHandler) Applicable(e Event) bool { return h.applicable(e) }

func (h *reactionHandler) Handle(ctx context.Context, e Event) error {
	stored, err := h.messages.Get(ctx, e.Repository, e.PR.Number)
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

	mapping, err := h.mappings.Get(ctx, e.Repository)
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

	if h.detector.ShouldSuppress(e.Sender.Type) {
		h.logger.Debug("skipped bot reviewer reaction",
			slog.String("login", e.Sender.Login),
			slog.String("event", e.GitHubEvent),
			slog.String("handler", h.name),
			slog.String("repository", e.Repository),
			slog.Int("pr", e.PR.Number),
		)
		return nil
	}

	// The slack.Client treats Slack's "already_reacted" error as a non-error,
	// so AddReaction is naturally idempotent. We don't need a GetReactions
	// pre-check (the PHP service did one, but it's redundant once the client
	// handles the duplicate-add case directly).
	if err := h.slack.AddReaction(ctx, mapping.SlackChannel, stored.TS, h.emoji); err != nil {
		return err
	}

	// A bot reviewer that survived the suppression gate above gets a distinct
	// marker alongside the normal state reaction, so its activity stays visible
	// but recognisably non-human. Empty botEmoji turns the marker off.
	if h.botEmoji != "" && h.detector.IsBot(e.Sender.Type) {
		return h.slack.AddReaction(ctx, mapping.SlackChannel, stored.TS, h.botEmoji)
	}
	return nil
}

// ApproveHandler adds a reaction when a review is submitted with state
// "approved".
type ApproveHandler struct{ reactionHandler }

// NewApproveHandler builds an ApproveHandler. detector must be non-nil; pass
// aireview.NewDetector(false) for the disabled state.
func NewApproveHandler(
	messages SlackMessages,
	mappings RepoMappings,
	slackClient SlackClient,
	logger *slog.Logger,
	emoji string,
	botEmoji string,
	detector *aireview.Detector,
) *ApproveHandler {
	return &ApproveHandler{reactionHandler{
		name:     "approve",
		emoji:    emoji,
		botEmoji: botEmoji,
		messages: messages, mappings: mappings, slack: slackClient, logger: logger, detector: detector,
		applicable: func(e Event) bool {
			return e.Action == "submitted" && e.Review != nil && e.Review.State == "approved"
		},
	}}
}

// CommentedHandler adds a reaction when a review is submitted or edited with
// state "commented".
type CommentedHandler struct{ reactionHandler }

// NewCommentedHandler builds a CommentedHandler. detector must be non-nil;
// pass aireview.NewDetector(false) for the disabled state.
func NewCommentedHandler(
	messages SlackMessages,
	mappings RepoMappings,
	slackClient SlackClient,
	logger *slog.Logger,
	emoji string,
	botEmoji string,
	detector *aireview.Detector,
) *CommentedHandler {
	return &CommentedHandler{reactionHandler{
		name:     "commented",
		emoji:    emoji,
		botEmoji: botEmoji,
		messages: messages, mappings: mappings, slack: slackClient, logger: logger, detector: detector,
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

// NewRequestChangeHandler builds a RequestChangeHandler. detector must be
// non-nil; pass aireview.NewDetector(false) for the disabled state.
func NewRequestChangeHandler(
	messages SlackMessages,
	mappings RepoMappings,
	slackClient SlackClient,
	logger *slog.Logger,
	emoji string,
	botEmoji string,
	detector *aireview.Detector,
) *RequestChangeHandler {
	return &RequestChangeHandler{reactionHandler{
		name:     "request_change",
		emoji:    emoji,
		botEmoji: botEmoji,
		messages: messages, mappings: mappings, slack: slackClient, logger: logger, detector: detector,
		applicable: func(e Event) bool {
			return e.Action == "submitted" && e.Review != nil && e.Review.State == "changes_requested"
		},
	}}
}
