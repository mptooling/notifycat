// Package feed wires the feed domain — agent subscriptions and the open pull
// request list — into an fx module. This file is the only fx-aware part of the domain.
package feed

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/feed/application"
	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/feed/infrastructure"
)

// Module binds the feed ports to their adapters and the use cases. It
// expects the composition root to supply a domain.Settings, a
// domain.DayOffCalendar, *persistence.FeedSubscriptions,
// *persistence.FeedPullRequests, *persistence.FeedDeliveries, *persistence.FeedPolls,
// routingdomain.EntryLister and a *slog.Logger.
var Module = fx.Module("feed",
	fx.Provide(
		fx.Annotate(infrastructure.NewSubscriptionRepo, fx.As(new(domain.SubscriptionStore))),
		fx.Annotate(infrastructure.NewPullRequestRepo, fx.As(new(domain.PullRequestStore))),
		fx.Annotate(infrastructure.NewDeliveryRepo, fx.As(new(domain.DeliveryStore))),
		fx.Annotate(infrastructure.NewPollRepo, fx.As(new(domain.PollStore))),
		fx.Annotate(infrastructure.NewConfigPartySettings, fx.As(new(domain.PartySettingsResolver))),
		fx.Annotate(infrastructure.NewRoutingCatalog, fx.As(new(domain.RepositoryCatalog))),
		fx.Annotate(infrastructure.NewSystemClock, fx.As(new(domain.Clock))),
		fx.Annotate(provideSubscriptions, fx.As(new(domain.Subscriptions))),
		fx.Annotate(providePullRequests, fx.As(new(domain.PullRequests))),
		fx.Annotate(application.NewRecorder, fx.As(new(domain.Recorder))),
		fx.Annotate(application.NewPullRequestCleaner, fx.As(new(domain.PullRequestCleaner))),
		fx.Annotate(application.NewSubscriptionCleaner, fx.As(new(domain.SubscriptionCleaner))),
		fx.Annotate(application.NewTurnPolicy, fx.As(new(domain.TurnPolicy))),
		fx.Annotate(provideWorkCalendar, fx.As(new(domain.WorkCalendar))),
	),
)

func provideWorkCalendar(settings domain.Settings, daysOff domain.DayOffCalendar) *application.WorkCalendar {
	return application.NewWorkCalendar(domain.WorkCalendarParams{DaysOff: daysOff, Location: settings.Location})
}

type subscriptionsParams struct {
	fx.In
	Subscriptions domain.SubscriptionStore
	Catalog       domain.RepositoryCatalog
	Clock         domain.Clock
	Logger        *slog.Logger
}

func provideSubscriptions(params subscriptionsParams) *application.Subscriptions {
	return application.NewSubscriptions(domain.SubscriptionsParams{
		Subscriptions: params.Subscriptions,
		Catalog:       params.Catalog,
		Clock:         params.Clock,
		Logger:        params.Logger,
	})
}

type pullRequestsParams struct {
	fx.In
	Subscriptions domain.SubscriptionStore
	PullRequests  domain.PullRequestStore
	Deliveries    domain.DeliveryStore
	Polls         domain.PollStore
	Catalog       domain.RepositoryCatalog
	Calendar      domain.WorkCalendar
	Clock         domain.Clock
	Logger        *slog.Logger
}

func providePullRequests(params pullRequestsParams) *application.PullRequests {
	return application.NewPullRequests(domain.PullRequestsParams{
		Subscriptions: params.Subscriptions,
		PullRequests:  params.PullRequests,
		Deliveries:    params.Deliveries,
		Polls:         params.Polls,
		Catalog:       params.Catalog,
		Calendar:      params.Calendar,
		Clock:         params.Clock,
		Logger:        params.Logger,
	})
}
