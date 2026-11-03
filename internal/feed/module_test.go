package feed_test

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/feed"
	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

type noMappings struct{}

func (noMappings) Entries() []routingdomain.Entry { return nil }

type noDaysOff struct{}

func (noDaysOff) IsDayOff(time.Time) bool { return false }

func TestModule_GraphResolves(t *testing.T) {
	db := persistence.NewTestDB(t)

	app := fx.New(
		feed.Module,
		fx.NopLogger,
		fx.Provide(
			func() *persistence.FeedSubscriptions { return persistence.NewFeedSubscriptions(db) },
			func() *persistence.FeedPullRequests { return persistence.NewFeedPullRequests(db) },
			func() *persistence.FeedDeliveries { return persistence.NewFeedDeliveries(db) },
			func() *persistence.FeedPolls { return persistence.NewFeedPolls(db) },
			func() domain.Settings { return domain.Settings{Location: time.UTC} },
			func() domain.DayOffCalendar { return noDaysOff{} },
			func() routingdomain.EntryLister { return noMappings{} },
			func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) },
		),
		fx.Invoke(func(domain.Subscriptions, domain.PullRequests, domain.Recorder, domain.PullRequestCleaner, domain.WorkCalendar) {
		}),
	)

	assert.NoError(t, app.Err())
}
