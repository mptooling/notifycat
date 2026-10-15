// Package notification wires the notification domain — the core PR-event → chat
// notification flow — into an fx module. This file is the only fx-aware part of
// the domain; the domain, application, and infrastructure layers stay
// framework-free.
package notification

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/notification/infrastructure"
)

// Module binds the notification ports to their adapters and use cases, and
// assembles the six lifecycle handlers into a value group the dispatcher
// consumes. It expects the composition root to supply the external inputs it
// cannot build itself: the store's *store.PullRequests, *store.CodeReviews and
// *store.ScheduledNotifications, a *slack.Client and *slack.Composer, the
// routing provider (as domain.RepoBehavior and domain.TargetResolver), and a
// *slog.Logger.
var Module = fx.Module("notification",
	fx.Provide(
		fx.Annotate(infrastructure.NewMessageRepo, fx.As(new(domain.MessageStore))),
		fx.Annotate(infrastructure.NewSlackMessenger, fx.As(new(domain.Messenger))),
		fx.Annotate(infrastructure.NewScheduledQueueRepo, fx.As(new(domain.ScheduledQueue))),
		fx.Annotate(infrastructure.NewSlackScheduledPoster, fx.As(new(domain.ScheduledPoster))),
		fx.Annotate(provideScheduledSender, fx.As(new(domain.ScheduledSender))),
		fx.Annotate(provideOpenHandler, fx.As(new(domain.Handler)), fx.ResultTags(`group:"handlers"`)),
		fx.Annotate(application.NewCloseHandler, fx.As(new(domain.Handler)), fx.ResultTags(`group:"handlers"`)),
		fx.Annotate(application.NewDraftHandler, fx.As(new(domain.Handler)), fx.ResultTags(`group:"handlers"`)),
		fx.Annotate(application.NewApproveHandler, fx.As(new(domain.Handler)), fx.ResultTags(`group:"handlers"`)),
		fx.Annotate(application.NewCommentedHandler, fx.As(new(domain.Handler)), fx.ResultTags(`group:"handlers"`)),
		fx.Annotate(application.NewRequestChangeHandler, fx.As(new(domain.Handler)), fx.ResultTags(`group:"handlers"`)),
		fx.Annotate(provideDispatcher, fx.As(new(domain.EventDispatcher))),
	),
)

type openHandlerParams struct {
	fx.In
	Store     domain.MessageStore
	Resolver  domain.TargetResolver
	Messenger domain.Messenger
	Queue     domain.ScheduledQueue
	Logger    *slog.Logger
}

func provideOpenHandler(params openHandlerParams) *application.OpenHandler {
	return application.NewOpenHandler(domain.OpenHandlerParams{
		Store:     params.Store,
		Resolver:  params.Resolver,
		Messenger: params.Messenger,
		Queue:     params.Queue,
		Logger:    params.Logger,
	})
}

type scheduledSenderParams struct {
	fx.In
	Queue    domain.ScheduledQueue
	Behavior domain.RepoBehavior
	Poster   domain.ScheduledPoster
	Logger   *slog.Logger
}

func provideScheduledSender(params scheduledSenderParams) *application.ScheduledSender {
	return application.NewScheduledSender(domain.ScheduledSenderParams{
		Queue:    params.Queue,
		Behavior: params.Behavior,
		Poster:   params.Poster,
		Logger:   params.Logger,
	})
}

// dispatcherParams collects the handler value group for the dispatcher.
type dispatcherParams struct {
	fx.In
	Handlers []domain.Handler `group:"handlers"`
}

// provideDispatcher builds the dispatcher from the assembled handler group.
func provideDispatcher(logger *slog.Logger, params dispatcherParams) *application.Dispatcher {
	return application.NewDispatcher(logger, params.Handlers)
}
