package runtime_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"gorm.io/gorm"

	notificationapp "github.com/mptooling/notifycat/internal/notification/application"
	notificationdomain "github.com/mptooling/notifycat/internal/notification/domain"
	notificationinfra "github.com/mptooling/notifycat/internal/notification/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	"github.com/mptooling/notifycat/internal/platform/slack"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/runtime"
)

const (
	scheduledChannel = "C0SCHEDULE"
	perEventChannel  = "C0PEREVENT"
)

func hourlyScheduled() *routingdomain.NotificationsConfig {
	return &routingdomain.NotificationsConfig{
		Strategy: routingdomain.StrategyScheduled,
		Mode:     routingdomain.ScheduleModeHourly,
	}
}

func useScheduledStrategy(repository string) func(*config.Config) {
	return func(cfg *config.Config) {
		org, repo, _ := splitRepository(repository)
		tier := cfg.Mappings[org][repo]
		tier.Notifications = hourlyScheduled()
		cfg.Mappings[org][repo] = tier
	}
}

func openedPayload(repository string, prNumber int) string {
	return fmt.Sprintf(`{
		"action": "opened",
		"repository": {"full_name": %[1]q},
		"pull_request": {
			"number": %[2]d, "title": "fix", "html_url": "https://gh/%[1]s/pull/%[2]d",
			"user": {"login": "bob"}, "draft": false,
			"created_at": "2026-06-05T14:04:00Z"
		}
	}`, repository, prNumber)
}

// sendHourlyTick runs one tick of the scheduled sender against the fixture's
// database and Slack fake, as the runner does when the hourly cron fires.
func (f *integrationFixture) sendHourlyTick(t *testing.T) {
	t.Helper()

	db, err := persistence.Open(f.cfg.DatabaseURL)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, closeErr := persistence.SQLDB(db); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	slackClient := slack.NewClient(http.DefaultClient, f.cfg.SlackBotToken.Reveal(), slack.WithBaseURL(f.slack.URL))
	sender := notificationapp.NewScheduledSender(notificationdomain.ScheduledSenderParams{
		Queue:    notificationinfra.NewScheduledQueueRepo(persistence.NewScheduledNotifications(db)),
		Behavior: routingapp.NewProvider(routingdomain.Defaults{}, f.cfg.Mappings, f.cfg.Digest),
		Poster:   notificationinfra.NewSlackScheduledPoster(slackClient, slack.NewComposer(f.cfg.Reactions.NewPR, f.cfg.SlackInteractivityEnabled())),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:      time.Now,
	})

	require.NoError(t, sender.SendSchedule(context.Background(), routingdomain.HourlyCronSpec))
}

func (f *slackFake) postsTo(channel string) []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var posts []fakeCall
	for _, call := range f.calls {
		if call.Path == "/api/chat.postMessage" && call.Body["channel"] == channel {
			posts = append(posts, call)
		}
	}
	return posts
}

func TestIntegration_ScheduledRepoQueuesInsteadOfPosting(t *testing.T) {
	fixture := newIntegrationFixtureCfg(t,
		useScheduledStrategy("octo/widget"),
		mappingSeed{repository: "octo/widget", channel: scheduledChannel},
	)

	status := fixture.post(t, openedPayload("octo/widget", 42))

	require.Equal(t, http.StatusOK, status)
	assert.Empty(t, fixture.slack.postsTo(scheduledChannel), "a scheduled repo posts nothing on the event")
}

func TestIntegration_ScheduledTickPostsOneListPerChannel(t *testing.T) {
	fixture := newIntegrationFixtureCfg(t,
		useScheduledStrategy("octo/widget"),
		mappingSeed{repository: "octo/widget", channel: scheduledChannel},
	)
	require.Equal(t, http.StatusOK, fixture.post(t, openedPayload("octo/widget", 42)))
	require.Equal(t, http.StatusOK, fixture.post(t, openedPayload("octo/widget", 43)))

	fixture.sendHourlyTick(t)

	posts := fixture.slack.postsTo(scheduledChannel)
	require.Len(t, posts, 1, "both PRs go out in one list message")
	list := blockText(posts[0].Body, "section")
	assert.Contains(t, list, "2 PRs ready for review")
	assert.Equal(t, "2 PRs ready for review", posts[0].Body["text"])
}

func TestIntegration_ScheduledTickPostsToEveryFannedOutChannel(t *testing.T) {
	fixture := newIntegrationFixtureCfg(t,
		func(cfg *config.Config) {
			tier := cfg.Mappings["octo"]["widget"]
			tier.Channel = ""
			tier.Channels = []routingdomain.ChannelSpec{
				{Channel: scheduledChannel, Mentions: []string{"<!subteam^S1>"}, MentionsPresent: true},
				{Channel: perEventChannel, Mentions: []string{"<!subteam^S2>"}, MentionsPresent: true},
			}
			tier.Notifications = hourlyScheduled()
			cfg.Mappings["octo"]["widget"] = tier
		},
		mappingSeed{repository: "octo/widget", channel: scheduledChannel},
	)
	require.Equal(t, http.StatusOK, fixture.post(t, openedPayload("octo/widget", 42)))

	fixture.sendHourlyTick(t)

	firstChannelPosts := fixture.slack.postsTo(scheduledChannel)
	secondChannelPosts := fixture.slack.postsTo(perEventChannel)
	require.Len(t, firstChannelPosts, 1)
	require.Len(t, secondChannelPosts, 1)
	assert.Contains(t, blockText(firstChannelPosts[0].Body, "section"), "<!subteam^S1>")
	assert.Contains(t, blockText(secondChannelPosts[0].Body, "section"), "<!subteam^S2>")
}

func TestIntegration_ScheduledTickSkipsPRsClosedOrDraftedBeforeTheSend(t *testing.T) {
	testCases := []struct {
		name          string
		deleteOnClose bool
		payload       string
	}{
		{name: "merged", payload: closeMergedPayload},
		{name: "merged with delete on close", deleteOnClose: true, payload: closeMergedPayload},
		{name: "converted to draft", payload: `{
			"action": "converted_to_draft",
			"repository": {"full_name": "octo/widget"},
			"pull_request": {"number": 42, "title": "fix", "html_url": "u", "user": {"login": "bob"}, "draft": true}
		}`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newIntegrationFixtureCfg(t,
				func(cfg *config.Config) {
					cfg.DeleteOnClose = testCase.deleteOnClose
					useScheduledStrategy("octo/widget")(cfg)
				},
				mappingSeed{repository: "octo/widget", channel: scheduledChannel},
			)
			require.Equal(t, http.StatusOK, fixture.post(t, openedPayload("octo/widget", 42)))
			require.Equal(t, http.StatusOK, fixture.post(t, testCase.payload))

			fixture.sendHourlyTick(t)

			assert.Empty(t, fixture.slack.postsTo(scheduledChannel))
			assert.Empty(t, fixture.slack.paths(), "a queued PR makes no Slack call on close or draft")
		})
	}
}

func TestIntegration_ScheduledTickDoesNotRepeatSentPRs(t *testing.T) {
	fixture := newIntegrationFixtureCfg(t,
		useScheduledStrategy("octo/widget"),
		mappingSeed{repository: "octo/widget", channel: scheduledChannel},
	)
	require.Equal(t, http.StatusOK, fixture.post(t, openedPayload("octo/widget", 42)))
	fixture.sendHourlyTick(t)

	fixture.sendHourlyTick(t)

	assert.Len(t, fixture.slack.postsTo(scheduledChannel), 1, "an empty queue posts nothing")
}

func TestIntegration_PerEventRepoStillPostsAtOnce(t *testing.T) {
	fixture := newIntegrationFixtureCfg(t,
		useScheduledStrategy("octo/widget"),
		mappingSeed{repository: "octo/widget", channel: scheduledChannel},
		mappingSeed{repository: "octo/gadget", channel: perEventChannel},
	)

	status := fixture.post(t, openedPayload("octo/gadget", 7))

	require.Equal(t, http.StatusOK, status)
	assert.Len(t, fixture.slack.postsTo(perEventChannel), 1)
	assert.Empty(t, fixture.slack.postsTo(scheduledChannel))
}

func TestWire_BuildsScheduledRunnerOnlyForScheduledMappings(t *testing.T) {
	testCases := []struct {
		name       string
		mutate     func(*config.Config)
		wantRunner bool
	}{
		{name: "per-event mappings", mutate: func(*config.Config) {}, wantRunner: false},
		{name: "scheduled mapping", mutate: useScheduledStrategy("octo/widget"), wantRunner: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := newTestConfig(t)
			cfg.Mappings = seedsToMappings(t, []mappingSeed{{repository: "octo/widget", channel: scheduledChannel}})
			testCase.mutate(&cfg)
			primeLock(t, cfg.ConfigFile, routingapp.NewProvider(routingdomain.Defaults{}, cfg.Mappings, cfg.Digest))
			var runner *notificationapp.ScheduledRunner
			var db *gorm.DB

			app := fx.New(fx.Supply(cfg), runtime.Module, fx.NopLogger, fx.Populate(&runner, &db))

			require.NoError(t, app.Err())
			t.Cleanup(func() {
				if sqlDB, closeErr := persistence.SQLDB(db); closeErr == nil {
					_ = sqlDB.Close()
				}
			})
			assert.Equal(t, testCase.wantRunner, runner != nil)
		})
	}
}
