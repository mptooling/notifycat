package startreview

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/mptooling/notifycat/internal/slackhook"
	"github.com/mptooling/notifycat/internal/store"
)

const startReviewActionID = "start_review"

// Handler consumes a parsed Slack interaction and, for a "start_review" click,
// records the click's user as a reviewer and appends a marker line to the PR
// message. Multiple distinct users may review the same PR; a user clicking
// again is a no-op (guarded in the app and enforced by the DB unique index).
type Handler struct {
	reviews  Reviews
	messages Messages
	slack    SlackUpdater
	composer Composer
	logger   *slog.Logger
	now      func() time.Time
}

// NewHandler builds a Handler. now is injected for deterministic tests.
func NewHandler(reviews Reviews, messages Messages, slackUpdater SlackUpdater, composer Composer, logger *slog.Logger, now func() time.Time) *Handler {
	return &Handler{reviews: reviews, messages: messages, slack: slackUpdater, composer: composer, logger: logger, now: now}
}

// Handle implements slackhook.InteractionSink. Unactionable input is logged and
// ignored (returns nil); a returned error is reserved for genuine infrastructure
// failures. The HTTP layer responds 200 regardless.
func (h *Handler) Handle(ctx context.Context, interaction slackhook.Interaction) error {
	if interaction.Type != "block_actions" {
		return nil
	}
	action, ok := firstAction(interaction)
	if !ok || action.ActionID != startReviewActionID {
		return nil
	}

	repository, prNumber, ok := decodeValue(action.Value)
	if !ok {
		h.logger.Debug("ignored start_review",
			slog.String("reason", "malformed_value"), slog.String("value", action.Value))
		return nil
	}

	if _, err := h.messages.Messages(ctx, repository, prNumber); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			h.logger.Info("ignored start_review",
				slog.String("reason", "no_stored_message"),
				slog.String("repository", repository), slog.Int("pr", prNumber))
			return nil
		}
		return err
	}

	userID := interaction.User.ID
	userName := interaction.User.Username

	if _, err := h.reviews.ActiveForUser(ctx, repository, prNumber, userID); err == nil {
		h.logger.Debug("duplicate start_review ignored",
			slog.String("reason", "already_reviewing"),
			slog.String("repository", repository), slog.Int("pr", prNumber), slog.String("user", userID))
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	if err := h.reviews.Start(ctx, repository, prNumber, userID, userName); err != nil {
		if errors.Is(err, store.ErrActiveReviewExists) {
			h.logger.Debug("duplicate start_review ignored",
				slog.String("reason", "db_conflict"),
				slog.String("repository", repository), slog.Int("pr", prNumber), slog.String("user", userID))
			return nil
		}
		return err
	}

	marker := h.composer.ReviewingMarker(userID, h.now())
	rawMarker, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	blocks := insertBeforeActions(splitBlocks(interaction.Message.RawBlocks), rawMarker)

	if err := h.slack.UpdateMessageRawBlocks(ctx, interaction.Channel.ID, interaction.Message.TS, blocks, interaction.Message.Text); err != nil {
		// The review is recorded; a failed cosmetic update is logged, not
		// compensated — a later update reconciles the message.
		h.logger.Warn("start_review recorded but message update failed",
			slog.String("repository", repository), slog.Int("pr", prNumber), slog.Any("err", err))
		return nil
	}
	h.logger.Info("start_review recorded",
		slog.String("repository", repository), slog.Int("pr", prNumber), slog.String("user", userID))
	return nil
}

func firstAction(interaction slackhook.Interaction) (slackhook.Action, bool) {
	if len(interaction.Actions) == 0 {
		return slackhook.Action{}, false
	}
	return interaction.Actions[0], true
}

// decodeValue parses the button value "repository#number" (e.g. "octo/web#42").
// A GitHub repository name cannot contain '#', so the last '#' splits repo from
// PR number unambiguously.
func decodeValue(value string) (string, int, bool) {
	hashIndex := strings.LastIndex(value, "#")
	if hashIndex <= 0 || hashIndex == len(value)-1 {
		return "", 0, false
	}
	repository := value[:hashIndex]
	prNumber, err := strconv.Atoi(value[hashIndex+1:])
	if err != nil || prNumber <= 0 {
		return "", 0, false
	}
	return repository, prNumber, true
}

// splitBlocks decodes a raw Slack blocks array into its element raws. A missing
// or malformed array yields nil, so the update still posts the marker alone.
func splitBlocks(raw json.RawMessage) []json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}
	return blocks
}

// insertBeforeActions places marker immediately before the first "actions" block
// (the button row) so the reviewer line renders above the button; with no
// actions block it appends.
func insertBeforeActions(blocks []json.RawMessage, marker json.RawMessage) []json.RawMessage {
	for i, block := range blocks {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(block, &probe); err == nil && probe.Type == "actions" {
			out := make([]json.RawMessage, 0, len(blocks)+1)
			out = append(out, blocks[:i]...)
			out = append(out, marker)
			out = append(out, blocks[i:]...)
			return out
		}
	}
	return append(blocks, marker)
}
