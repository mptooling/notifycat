package pullrequest

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// OpenHandler reacts to a PR being opened (non-draft) or marked
// ready_for_review. It posts the first Slack message for the PR and records
// the message TS for later updates.
type OpenHandler struct {
	messages SlackMessages
	mappings RepoMappings
	slack    SlackClient
	composer *slack.Composer
	logger   *slog.Logger
}

// NewOpenHandler builds an OpenHandler.
func NewOpenHandler(
	messages SlackMessages,
	mappings RepoMappings,
	slackClient SlackClient,
	composer *slack.Composer,
	logger *slog.Logger,
) *OpenHandler {
	return &OpenHandler{
		messages: messages,
		mappings: mappings,
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
		h.logger.Info("message already sent",
			slog.String("repository", e.Repository),
			slog.Int("pr", e.PR.Number),
		)
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	mapping, err := h.mappings.Get(ctx, e.Repository)
	if errors.Is(err, store.ErrNotFound) {
		h.logger.Warn("no slack mapping for repository", slog.String("repository", e.Repository))
		return nil
	}
	if err != nil {
		return err
	}

	text := h.composer.NewMessage(slackPRFrom(e), mapping.Mentions)
	ts, err := h.slack.PostMessage(ctx, mapping.SlackChannel, text)
	if err != nil {
		return err
	}

	return h.messages.Save(ctx, store.SlackMessage{
		PRNumber:   e.PR.Number,
		Repository: e.Repository,
		TS:         ts,
	})
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
	}
}
