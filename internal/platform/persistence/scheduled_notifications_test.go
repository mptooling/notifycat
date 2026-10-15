package persistence_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

type scheduledFixture struct {
	db           *gorm.DB
	pullRequests *persistence.PullRequests
	scheduled    *persistence.ScheduledNotifications
}

func newScheduledFixture(t *testing.T) scheduledFixture {
	t.Helper()

	db := persistence.NewTestDB(t)
	return scheduledFixture{
		db:           db,
		pullRequests: persistence.NewPullRequests(db),
		scheduled:    persistence.NewScheduledNotifications(db),
	}
}

func scheduledItem(repository string, prNumber int, channel string) persistence.ScheduledItem {
	return persistence.ScheduledItem{
		Repository: repository,
		PRNumber:   prNumber,
		Channel:    channel,
		Mentions:   []string{"<!subteam^S1>"},
		Title:      "Add retries",
		URL:        "https://github.com/acme/web/pull/7",
		Author:     "octocat",
	}
}

func TestScheduledNotifications_EnqueueIsIdempotentPerChannel(t *testing.T) {
	fixture := newScheduledFixture(t)
	ctx := context.Background()
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 7, "C0A")))
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 7, "C0A")))
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 7, "C0B")))

	pending, err := fixture.scheduled.Pending(ctx)

	require.NoError(t, err)
	assert.Len(t, pending, 2, "one row per channel, the repeat is deduped")
}

func TestScheduledNotifications_EnqueueTracksThePullRequest(t *testing.T) {
	fixture := newScheduledFixture(t)
	ctx := context.Background()
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 7, "C0A")))

	messages, err := fixture.pullRequests.Messages(ctx, "acme/web", 7)

	require.NoError(t, err, "close and draft handlers find the PR row")
	assert.Empty(t, messages, "nothing was posted to Slack yet")
}

func TestScheduledNotifications_PendingCarriesTheItem(t *testing.T) {
	fixture := newScheduledFixture(t)
	ctx := context.Background()
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 7, "C0A")))

	pending, err := fixture.scheduled.Pending(ctx)

	require.NoError(t, err)
	require.Len(t, pending, 1)
	want := scheduledItem("acme/web", 7, "C0A")
	want.ID = pending[0].ID
	want.QueuedAt = pending[0].QueuedAt
	assert.Equal(t, want, pending[0])
	assert.NotZero(t, pending[0].ID)
	assert.WithinDuration(t, time.Now(), pending[0].QueuedAt, time.Minute)
}

func TestScheduledNotifications_PendingInQueueOrder(t *testing.T) {
	fixture := newScheduledFixture(t)
	ctx := context.Background()
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 9, "C0A")))
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/api", 3, "C0A")))

	pending, err := fixture.scheduled.Pending(ctx)

	require.NoError(t, err)
	require.Len(t, pending, 2)
	assert.Equal(t, 9, pending[0].PRNumber)
	assert.Equal(t, 3, pending[1].PRNumber)
}

func TestScheduledNotifications_PendingSkipsClosedPullRequests(t *testing.T) {
	fixture := newScheduledFixture(t)
	ctx := context.Background()
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 7, "C0A")))
	require.NoError(t, fixture.pullRequests.MarkClosed(ctx, "acme/web", 7))

	pending, err := fixture.scheduled.Pending(ctx)

	require.NoError(t, err)
	assert.Empty(t, pending, "a PR closed before the send is not announced")
}

func TestScheduledNotifications_MarkSentRemovesFromPending(t *testing.T) {
	fixture := newScheduledFixture(t)
	ctx := context.Background()
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 7, "C0A")))
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 8, "C0A")))
	queued, err := fixture.scheduled.Pending(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 2)

	require.NoError(t, fixture.scheduled.MarkSent(ctx, []uint{queued[0].ID}, time.Now()))

	pending, err := fixture.scheduled.Pending(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, 8, pending[0].PRNumber)
}

func TestScheduledNotifications_RedeliveryAfterSendIsNotRequeued(t *testing.T) {
	fixture := newScheduledFixture(t)
	ctx := context.Background()
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 7, "C0A")))
	queued, err := fixture.scheduled.Pending(ctx)
	require.NoError(t, err)
	require.NoError(t, fixture.scheduled.MarkSent(ctx, []uint{queued[0].ID}, time.Now()))

	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 7, "C0A")))

	pending, err := fixture.scheduled.Pending(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending, "a PR is announced in a channel once")
}

func TestScheduledNotifications_DeletingThePullRequestCascades(t *testing.T) {
	fixture := newScheduledFixture(t)
	ctx := context.Background()
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 7, "C0A")))

	require.NoError(t, fixture.pullRequests.Delete(ctx, "acme/web", 7))

	var count int64
	require.NoError(t, fixture.db.Raw("SELECT count(*) FROM scheduled_notifications").Scan(&count).Error)
	assert.Zero(t, count)
}

func TestScheduledNotifications_AnnouncedSince(t *testing.T) {
	fixture := newScheduledFixture(t)
	ctx := context.Background()
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 7, "C0A")))
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 8, "C0A")))
	require.NoError(t, fixture.scheduled.Enqueue(ctx, scheduledItem("acme/web", 9, "C0A")))
	queued, err := fixture.scheduled.Pending(ctx)
	require.NoError(t, err)
	require.Len(t, queued, 3)
	cutoff := time.Now()
	require.NoError(t, fixture.scheduled.MarkSent(ctx, []uint{queued[1].ID}, cutoff.Add(-time.Hour)))
	require.NoError(t, fixture.scheduled.MarkSent(ctx, []uint{queued[2].ID}, cutoff.Add(time.Minute)))

	announced, err := fixture.scheduled.AnnouncedSince(ctx, cutoff)

	require.NoError(t, err)
	numbers := make([]int, 0, len(announced))
	for _, item := range announced {
		numbers = append(numbers, item.PRNumber)
	}
	assert.ElementsMatch(t, []int{7, 9}, numbers, "unsent and sent-after-cutoff rows count; sent-before-cutoff does not")
}
