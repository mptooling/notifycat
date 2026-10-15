package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

const (
	nineAM  = "0 9 * * *"
	threePM = "0 15 * * *"
)

var sendTime = time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)

func cronNotifications(specs ...string) routingdomain.RepoMapping {
	return routingdomain.RepoMapping{Notifications: routingdomain.NotificationsConfig{
		Strategy: routingdomain.StrategyScheduled,
		Mode:     routingdomain.ScheduleModeCron,
		Cron:     specs,
	}}
}

func queuedEntry(id uint, repository string, prNumber int, channel string, mentions ...string) domain.ScheduledEntry {
	return domain.ScheduledEntry{
		ID:         id,
		Repository: repository,
		PR:         kernel.PR{Number: prNumber},
		Channel:    channel,
		Mentions:   mentions,
	}
}

func newScheduledSender(queue *fakeScheduledQueue, behaviors fakeRepoBehaviors, poster *fakeScheduledPoster) *application.ScheduledSender {
	return application.NewScheduledSender(domain.ScheduledSenderParams{
		Queue:    queue,
		Behavior: behaviors,
		Poster:   poster,
		Logger:   discardLogger(),
		Now:      func() time.Time { return sendTime },
	})
}

func TestScheduledSender_PostsOneListPerChannelAndMarksSent(t *testing.T) {
	webOne := queuedEntry(1, "acme/web", 7, "C0A", "<@U0A>")
	apiOne := queuedEntry(2, "acme/api", 3, "C0B", "<@U0B>")
	webTwo := queuedEntry(3, "acme/web", 8, "C0A", "<@U0A>")
	queue := &fakeScheduledQueue{pending: []domain.ScheduledEntry{webOne, apiOne, webTwo}}
	behaviors := fakeRepoBehaviors{"acme/web": cronNotifications(nineAM), "acme/api": cronNotifications(nineAM)}
	poster := &fakeScheduledPoster{}

	err := newScheduledSender(queue, behaviors, poster).SendSchedule(context.Background(), nineAM)

	require.NoError(t, err)
	assert.Equal(t, []scheduledListCall{
		{channel: "C0A", req: domain.ScheduledListRequest{Mentions: []string{"<@U0A>"}, Entries: []domain.ScheduledEntry{webOne, webTwo}}},
		{channel: "C0B", req: domain.ScheduledListRequest{Mentions: []string{"<@U0B>"}, Entries: []domain.ScheduledEntry{apiOne}}},
	}, poster.posts)
	assert.ElementsMatch(t, []uint{1, 2, 3}, queue.sentIDs)
	assert.Equal(t, sendTime, queue.sentAt)
}

func TestScheduledSender_OnlyRepositoriesOnTheTickSpec(t *testing.T) {
	queue := &fakeScheduledQueue{pending: []domain.ScheduledEntry{
		queuedEntry(1, "acme/web", 7, "C0A"),
		queuedEntry(2, "acme/api", 3, "C0A"),
		queuedEntry(3, "acme/ops", 5, "C0A"),
	}}
	behaviors := fakeRepoBehaviors{
		"acme/web": cronNotifications(nineAM),
		"acme/api": cronNotifications(threePM),
		"acme/ops": cronNotifications(threePM, nineAM),
	}
	poster := &fakeScheduledPoster{}

	err := newScheduledSender(queue, behaviors, poster).SendSchedule(context.Background(), nineAM)

	require.NoError(t, err)
	require.Len(t, poster.posts, 1)
	assert.Len(t, poster.posts[0].req.Entries, 2)
	assert.ElementsMatch(t, []uint{1, 3}, queue.sentIDs, "the 3pm repo waits for its own tick")
}

func TestScheduledSender_SkipsRepositoriesNoLongerScheduled(t *testing.T) {
	queue := &fakeScheduledQueue{pending: []domain.ScheduledEntry{
		queuedEntry(1, "acme/web", 7, "C0A"),
		queuedEntry(2, "acme/gone", 3, "C0A"),
	}}
	behaviors := fakeRepoBehaviors{"acme/web": {Notifications: routingdomain.NotificationsConfig{Strategy: routingdomain.StrategyPerEvent}}}
	poster := &fakeScheduledPoster{}

	err := newScheduledSender(queue, behaviors, poster).SendSchedule(context.Background(), nineAM)

	require.NoError(t, err)
	assert.Empty(t, poster.posts)
	assert.Empty(t, queue.sentIDs)
}

func TestScheduledSender_UnionsMentionsPerChannel(t *testing.T) {
	queue := &fakeScheduledQueue{pending: []domain.ScheduledEntry{
		queuedEntry(1, "acme/web", 7, "C0A", "<@U0A>", "<@U0B>"),
		queuedEntry(2, "acme/web", 8, "C0A", "<@U0B>", "<@U0C>"),
	}}
	poster := &fakeScheduledPoster{}

	err := newScheduledSender(queue, fakeRepoBehaviors{"acme/web": cronNotifications(nineAM)}, poster).
		SendSchedule(context.Background(), nineAM)

	require.NoError(t, err)
	require.Len(t, poster.posts, 1)
	assert.Equal(t, []string{"<@U0A>", "<@U0B>", "<@U0C>"}, poster.posts[0].req.Mentions)
}

func TestScheduledSender_FailedChannelKeepsItsEntries(t *testing.T) {
	queue := &fakeScheduledQueue{pending: []domain.ScheduledEntry{
		queuedEntry(1, "acme/web", 7, "C0A"),
		queuedEntry(2, "acme/web", 8, "C0B"),
	}}
	poster := &fakeScheduledPoster{failChannels: map[string]bool{"C0A": true}}

	err := newScheduledSender(queue, fakeRepoBehaviors{"acme/web": cronNotifications(nineAM)}, poster).
		SendSchedule(context.Background(), nineAM)

	require.NoError(t, err)
	assert.Len(t, poster.posts, 2, "a failed channel does not stop the others")
	assert.Equal(t, []uint{2}, queue.sentIDs, "the failed channel retries on the next tick")
}

func TestScheduledSender_NothingPending(t *testing.T) {
	poster := &fakeScheduledPoster{}

	err := newScheduledSender(&fakeScheduledQueue{}, fakeRepoBehaviors{}, poster).SendSchedule(context.Background(), nineAM)

	require.NoError(t, err)
	assert.Empty(t, poster.posts)
}

func TestScheduledSender_PendingError(t *testing.T) {
	queue := &fakeScheduledQueue{pendingErr: errInjected}

	err := newScheduledSender(queue, fakeRepoBehaviors{}, &fakeScheduledPoster{}).SendSchedule(context.Background(), nineAM)

	require.ErrorIs(t, err, errInjected)
}
