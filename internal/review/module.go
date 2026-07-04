// Package review wires the review domain — the interactive "Start review" flow —
// into an fx module. This file is the only fx-aware part of the domain; the
// domain, application, and infrastructure layers stay framework-free.
package review

import (
	"log/slog"
	"time"

	"go.uber.org/fx"

	notificationdomain "github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/review/application"
	"github.com/mptooling/notifycat/internal/review/domain"
	"github.com/mptooling/notifycat/internal/review/infrastructure"
)

// Module binds the review ports to their adapters and the start-review use case.
// The code-reviews repository is bound to BOTH review's own Recorder port and the
// notification ReviewSessions port (review owns review sessions). It expects the
// composition root to supply the store's *store.CodeReviews and *store.PullRequests,
// a *slack.Composer and *slack.Client, and a *slog.Logger.
var Module = fx.Module("review",
	fx.Provide(
		fx.Annotate(infrastructure.NewCodeReviewsRepo,
			fx.As(new(domain.Recorder)),
			fx.As(new(notificationdomain.ReviewSessions)),
		),
		fx.Annotate(infrastructure.NewMessageChecker, fx.As(new(domain.MessageChecker))),
		fx.Annotate(infrastructure.NewSlackDecorator, fx.As(new(domain.MessageDecorator))),
		fx.Annotate(provideStartReview, fx.As(new(domain.StartReview))),
	),
)

// provideStartReview assembles the start-review use case from the review ports.
func provideStartReview(recorder domain.Recorder, messages domain.MessageChecker, decorator domain.MessageDecorator, logger *slog.Logger) *application.Handler {
	return application.NewHandler(domain.HandlerParams{
		Recorder:  recorder,
		Messages:  messages,
		Decorator: decorator,
		Logger:    logger,
		Now:       time.Now,
	})
}
