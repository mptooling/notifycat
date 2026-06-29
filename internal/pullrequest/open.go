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
// ready_for_review. It fans out one Slack message per resolved target channel
// and records each message for later updates.
type OpenHandler struct {
	store    PullRequestStore
	resolver TargetResolver
	messenger Messenger
	composer  *slack.Composer
	logger    *slog.Logger
}

// NewOpenHandler builds an OpenHandler.
func NewOpenHandler(
	store PullRequestStore,
	resolver TargetResolver,
	slackClient Messenger,
	composer *slack.Composer,
	logger *slog.Logger,
) *OpenHandler {
	return &OpenHandler{
		store:     store,
		resolver:  resolver,
		messenger: slackClient,
		composer:  composer,
		logger:    logger,
	}
}

// Applicable returns true for "ready_for_review", or "opened" on a non-draft PR.
func (h *OpenHandler) Applicable(e Event) bool {
	if e.Action == "ready_for_review" {
		return true
	}
	return e.Action == "opened" && !e.PR.Draft
}

// Handle posts one message per resolved target channel and records each. It is
// idempotent per channel: an existing message for a channel is skipped, so a
// redelivery or a partial-failure retry only posts the missing channels.
func (h *OpenHandler) Handle(ctx context.Context, e Event) error {
	behavior, targets, err := h.resolver.ResolveTargets(ctx, e.Repository, e.PR.Number)
	if errors.Is(err, store.ErrNotFound) {
		h.logIgnored(e, "no_mapping")
		return nil
	}
	if err != nil {
		return err
	}

	existing, err := h.store.Messages(ctx, e.Repository, e.PR.Number)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	already := map[string]bool{}
	for _, m := range existing {
		already[m.Channel] = true
	}

	for _, target := range targets {
		if already[target.Channel] {
			continue
		}
		msg := h.composeMessage(e, behavior, target.Mentions)
		messageID, err := h.messenger.PostMessage(ctx, target.Channel, msg)
		if err != nil {
			return err // successful channels are already saved; retry skips them
		}
		if err := h.store.AddMessage(ctx, e.Repository, e.PR.Number, target.Channel, messageID); err != nil {
			return err
		}
	}
	return nil
}

// composeMessage renders the message for an opened PR. When the dependabot
// format is enabled and the PR was opened by dependabot[bot]/renovate[bot], it
// picks the compact routine/security template; otherwise it uses the standard
// Block Kit message. Detection keys off the PR author, not the webhook sender:
// on a ready_for_review event the sender is the human who marked a bot's draft
// PR ready, while the author stays the bot.
func (h *OpenHandler) composeMessage(e Event, behavior store.RepoMapping, mentions []string) slack.Message {
	if behavior.DependabotFormat {
		if kind := botpr.DetectBot(e.PR.Author); kind != botpr.BotKindNone {
			return h.composer.BotMessage(slackPRFrom(e), mentions, kind.Name(), botpr.IsSecurityAdvisory(e.PR.Body))
		}
	}
	return h.composer.NewMessage(slackPRFrom(e), mentions, behavior.Reactions.NewPR)
}

func (h *OpenHandler) logIgnored(e Event, reason string) {
	h.logger.Warn("ignored webhook event",
		slog.String("reason", reason),
		slog.String("handler", "open"),
		slog.String("github_event", e.GitHubEvent),
		slog.String("action", e.Action),
		slog.String("repository", e.Repository),
		slog.Int("pr", e.PR.Number),
	)
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
