package pullrequest

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/botpr"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// OpenHandler reacts to a PR being opened (non-draft) or marked
// ready_for_review. It posts the first Slack message for the PR and records
// the message TS for later updates.
type OpenHandler struct {
	messages SlackMessages
	resolver Resolver
	slack    SlackClient
	composer *slack.Composer
	logger   *slog.Logger
}

// NewOpenHandler builds an OpenHandler.
func NewOpenHandler(
	messages SlackMessages,
	resolver Resolver,
	slackClient SlackClient,
	composer *slack.Composer,
	logger *slog.Logger,
) *OpenHandler {
	return &OpenHandler{
		messages: messages,
		resolver: resolver,
		slack:    slackClient,
		composer: composer,
		logger:   logger,
	}
}

// Applicable returns true for "ready_for_review", or "opened" on a non-draft PR.
func (h *OpenHandler) Applicable(e Event) bool {
	if e.Action == "ready_for_review" {
		return true
	}
	return e.Action == "opened" && !e.PR.Draft
}

// Handle posts the initial Slack message and stores its TS.
//
// Idempotency: if a SlackMessage already exists for this PR (same composite
// key) the handler returns silently — we never post twice for the same PR.
func (h *OpenHandler) Handle(ctx context.Context, e Event) error {
	if _, err := h.messages.Get(ctx, e.Repository, e.PR.Number); err == nil {
		h.logger.Info("ignored webhook event",
			slog.String("reason", "already_sent"),
			slog.String("handler", "open"),
			slog.String("github_event", e.GitHubEvent),
			slog.String("action", e.Action),
			slog.String("repository", e.Repository),
			slog.Int("pr", e.PR.Number),
		)
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	mapping, err := h.resolver.Resolve(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		h.logger.Warn("ignored webhook event",
			slog.String("reason", "no_mapping"),
			slog.String("handler", "open"),
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

	msg := h.composeMessage(e, mapping)
	ts, err := h.slack.PostMessage(ctx, mapping.SlackChannel, msg)
	if err != nil {
		return err
	}

	return h.messages.Save(ctx, store.SlackMessage{
		PRNumber:   e.PR.Number,
		Repository: e.Repository,
		TS:         ts,
	})
}

// composeMessage renders the message for an opened PR. When the dependabot
// format is enabled and the PR was opened by dependabot[bot]/renovate[bot], it
// picks the compact routine/security template; otherwise it uses the standard
// Block Kit message. Detection keys off the PR author, not the webhook sender:
// on a ready_for_review event the sender is the human who marked a bot's draft
// PR ready, while the author stays the bot.
func (h *OpenHandler) composeMessage(e Event, mapping store.RepoMapping) slack.Message {
	if mapping.DependabotFormat {
		if kind := botpr.DetectBot(e.PR.Author); kind != botpr.BotKindNone {
			security := botpr.IsSecurityAdvisory(e.PR.Body)
			return h.composer.BotMessage(slackPRFrom(e), mapping.Mentions, kind.Name(), security)
		}
	}
	return h.composer.NewMessage(slackPRFrom(e), mapping.Mentions, mapping.Reactions.NewPR)
}

// slackPRFrom adapts an Event's PR fields to the slack.PRDetails shape used
// by the composer. Centralising it keeps each handler trivial.
func slackPRFrom(e Event) slack.PRDetails {
	return slack.PRDetails{
		Repository: e.Repository,
		Number:     e.PR.Number,
		Title:      e.PR.Title,
		URL:        e.PR.URL,
		Author:     e.PR.Author,
		CreatedAt:  e.PR.CreatedAt,
	}
}
