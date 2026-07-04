package application

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/mptooling/notifycat/internal/review/domain"
)

// Handler records a reviewer and appends an in-review marker to the PR message
// on a verified "Start review" click.
type Handler struct {
	recorder  domain.Recorder
	messages  domain.MessageChecker
	decorator domain.MessageDecorator
	logger    *slog.Logger
	now       func() time.Time
}

// NewHandler builds a Handler from its params. Now defaults to time.Now.
func NewHandler(params domain.HandlerParams) *Handler {
	now := params.Now
	if now == nil {
		now = time.Now
	}
	return &Handler{
		recorder:  params.Recorder,
		messages:  params.Messages,
		decorator: params.Decorator,
		logger:    params.Logger,
		now:       now,
	}
}

// Handle implements domain.StartReview. Unactionable input is logged and ignored
// (returns nil); a returned error is reserved for genuine infrastructure
// failures. A duplicate click (same user) is a no-op.
func (h *Handler) Handle(ctx context.Context, command domain.StartReviewCommand) error {
	hasMessages, err := h.messages.HasMessages(ctx, command.Repository, command.PRNumber)
	if err != nil {
		return err
	}
	if !hasMessages {
		h.logger.Info("ignored start_review",
			slog.String("reason", "no_stored_message"),
			slog.String("repository", command.Repository), slog.Int("pr", command.PRNumber))
		return nil
	}

	active, err := h.recorder.HasActiveReview(ctx, command.Repository, command.PRNumber, command.Reviewer.UserID)
	if err != nil {
		return err
	}
	if active {
		h.logger.Debug("duplicate start_review ignored",
			slog.String("reason", "already_reviewing"),
			slog.String("repository", command.Repository), slog.Int("pr", command.PRNumber), slog.String("user", command.Reviewer.UserID))
		return nil
	}

	if err := h.recorder.Start(ctx, command.Repository, command.PRNumber, command.Reviewer.UserID, command.Reviewer.UserName); err != nil {
		if errors.Is(err, domain.ErrActiveReviewExists) {
			h.logger.Debug("duplicate start_review ignored",
				slog.String("reason", "db_conflict"),
				slog.String("repository", command.Repository), slog.Int("pr", command.PRNumber), slog.String("user", command.Reviewer.UserID))
			return nil
		}
		return err
	}

	if err := h.decorator.AppendReviewingMarker(ctx, command.Message, command.Reviewer, h.now()); err != nil {
		// The review is recorded; a failed cosmetic update is logged, not
		// compensated — a later update reconciles the message.
		h.logger.Warn("start_review recorded but message update failed",
			slog.String("repository", command.Repository), slog.Int("pr", command.PRNumber), slog.Any("err", err))
		return nil
	}
	h.logger.Info("start_review recorded",
		slog.String("repository", command.Repository), slog.Int("pr", command.PRNumber), slog.String("user", command.Reviewer.UserID))
	return nil
}

var _ domain.StartReview = (*Handler)(nil)
