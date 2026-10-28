package feed_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/feed"
	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

type noMappings struct{}

func (noMappings) Entries() []routingdomain.Entry { return nil }

func TestModule_GraphResolves(t *testing.T) {
	db := persistence.NewTestDB(t)

	app := fx.New(
		feed.Module,
		fx.NopLogger,
		fx.Provide(
			func() *persistence.FeedSubscriptions { return persistence.NewFeedSubscriptions(db) },
			func() *persistence.FeedPullRequests { return persistence.NewFeedPullRequests(db) },
			func() *persistence.FeedDeliveries { return persistence.NewFeedDeliveries(db) },
			func() routingdomain.EntryLister { return noMappings{} },
			func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) },
		),
		fx.Invoke(func(domain.Subscriptions, domain.PullRequests, domain.Recorder, domain.PullRequestCleaner) {}),
	)

	assert.NoError(t, app.Err())
}
