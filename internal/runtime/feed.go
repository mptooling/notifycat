package runtime

import (
	"context"
	"log/slog"

	feeddomain "github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/kernel"
	notificationapp "github.com/mptooling/notifycat/internal/notification/application"
	notificationdomain "github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/platform/config"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// recordingDispatcher records every webhook event for the agent feed after the
// notification dispatch, so a Slack failure still records and a record failure
// never changes the webhook response.
type recordingDispatcher struct {
	next     notificationdomain.EventDispatcher
	recorder feeddomain.Recorder
	logger   *slog.Logger
}

func (dispatcher *recordingDispatcher) Dispatch(ctx context.Context, event kernel.Event) error {
	dispatchErr := dispatcher.next.Dispatch(ctx, event)
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), feeddomain.RecordTimeout)
	defer cancel()
	if err := dispatcher.recorder.Record(recordCtx, event); err != nil {
		dispatcher.logger.WarnContext(ctx, "feed pull request not recorded",
			slog.String("repository", event.Repository),
			slog.Int("pr", event.PR.Number),
			slog.String("kind", event.Kind.String()),
			slog.Any("err", err))
	}
	return dispatchErr
}

// buildWebhookDispatcher records webhook events for the feed only when the
// agent API is enabled.
func buildWebhookDispatcher(cfg config.Config, dispatcher *notificationapp.Dispatcher, recorder feeddomain.Recorder, logger *slog.Logger) notificationdomain.EventDispatcher {
	if !cfg.AgentAPIEnabled {
		return dispatcher
	}
	return &recordingDispatcher{next: dispatcher, recorder: recorder, logger: logger}
}

// cleanFeed runs before serving. With the API off nothing records a close, and
// an unmapped repository records nothing, so those pull requests go stale;
// subscriptions to unmapped repositories go on every start. One step failing
// never skips the other.
func cleanFeed(cfg config.Config, pullRequests feeddomain.PullRequestCleaner, subscriptions feeddomain.SubscriptionCleaner, logger *slog.Logger) {
	cleanFeedPullRequests(cfg, pullRequests, logger)
	cleanFeedSubscriptions(subscriptions, logger)
}

func cleanFeedPullRequests(cfg config.Config, cleaner feeddomain.PullRequestCleaner, logger *slog.Logger) {
	remove, reason := cleaner.RemoveUnconfigured, feeddomain.CleanupReasonUnmapped
	if !cfg.AgentAPIEnabled {
		remove, reason = cleaner.RemoveAll, feeddomain.CleanupReasonAgentAPIOff
	}
	removed, err := remove(context.Background())
	if err != nil {
		logger.Warn("feed cleanup failed", slog.Any("err", err))
		return
	}
	if removed > 0 {
		logger.Info("feed pull requests removed", slog.Int("count", removed), slog.String("reason", string(reason)))
	}
}

func cleanFeedSubscriptions(cleaner feeddomain.SubscriptionCleaner, logger *slog.Logger) {
	removed, err := cleaner.RemoveUnconfigured(context.Background())
	if err != nil {
		logger.Warn("feed cleanup failed", slog.Any("err", err))
		return
	}
	if removed > 0 {
		logger.Info("feed subscriptions removed", slog.Int("count", removed), slog.String("reason", string(feeddomain.CleanupReasonUnmapped)))
	}
}

func provideEntryLister(provider *routingapp.Provider) routingdomain.EntryLister {
	return provider
}
