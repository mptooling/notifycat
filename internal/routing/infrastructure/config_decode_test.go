package infrastructure

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

func decodeOrg(t *testing.T, body string) map[string]repoConfigWire {
	t.Helper()

	org, err := decodeOrgWire(body)
	require.NoError(t, err)
	return org
}

func decodeOrgWire(body string) (map[string]repoConfigWire, error) {
	var org map[string]repoConfigWire
	decoder := yaml.NewDecoder(strings.NewReader(body))
	decoder.KnownFields(true)
	return org, decoder.Decode(&org)
}

func decodeOrgError(body string) error {
	_, err := decodeOrgWire(body)
	return err
}

func TestRepoConfig_ChannelAndMentionsPresent(t *testing.T) {
	org := decodeOrg(t, `
api:
  channel: C0API
  mentions: ["<@U1>"]
"*":
  channel: C0STAR
`)

	api, ok := org["api"]
	require.True(t, ok, "api tier decoded")
	assert.Equal(t, "C0API", api.Channel)
	assert.True(t, api.MentionsPresent)
	assert.Equal(t, []string{"<@U1>"}, api.Mentions)

	star := org["*"]
	assert.Equal(t, "C0STAR", star.Channel)
	assert.False(t, star.MentionsPresent, "star tier omits mentions")
}

func TestRepoConfig_EmptyMentionsIsPresent(t *testing.T) {
	org := decodeOrg(t, "api:\n  channel: C0API\n  mentions: []\n")

	assert.True(t, org["api"].MentionsPresent)
	assert.Empty(t, org["api"].Mentions)
}

func TestRepoConfig_NullMentionsRejected(t *testing.T) {
	err := decodeOrgError("api:\n  channel: C0API\n  mentions: null\n")

	require.Error(t, err)
}

func TestRepoConfig_UnknownKeyRejected(t *testing.T) {
	err := decodeOrgError("api:\n  channel: C0API\n  bogus: x\n")

	require.Error(t, err)
}

func TestRepoConfig_BehavioralOverrides(t *testing.T) {
	org := decodeOrg(t, `
api:
  channel: C0API
  reactions:
    approved: shipit
    enabled: false
  reviews:
    ignore_ai_reviews: true
    dependabot_format: false
  digest:
    enabled: false
    schedule: "0 8 * * 1-5"
`)

	api := org["api"]
	require.NotNil(t, api.Reactions)
	require.NotNil(t, api.Reactions.Approved)
	assert.Equal(t, "shipit", *api.Reactions.Approved)
	require.NotNil(t, api.Reactions.Enabled)
	assert.False(t, *api.Reactions.Enabled)
	require.NotNil(t, api.IgnoreAIReviews)
	assert.True(t, *api.IgnoreAIReviews)
	require.NotNil(t, api.DependabotFormat)
	assert.False(t, *api.DependabotFormat)
	require.NotNil(t, api.Digest)
	assert.False(t, api.Digest.Enabled)
	assert.Equal(t, "0 8 * * 1-5", api.Digest.Schedule)
}

func TestRepoConfig_BehavioralAbsentMeansNil(t *testing.T) {
	api := decodeOrg(t, "api:\n  channel: C0API\n")["api"]

	assert.Nil(t, api.Reactions, "absent behavioral keys stay nil so the tier inherits")
	assert.Nil(t, api.IgnoreAIReviews)
	assert.Nil(t, api.DependabotFormat)
	assert.Nil(t, api.Digest)
}

func TestRepoConfig_DigestTimezoneRejected(t *testing.T) {
	// timezone is a global-only knob (one cron location for the whole server);
	// setting it on a per-repo tier must fail rather than be silently ignored.
	err := decodeOrgError("api:\n  channel: C0API\n  digest:\n    timezone: Europe/Kyiv\n")

	require.Error(t, err)
}

func TestRepoConfig_UnknownReactionKeyRejected(t *testing.T) {
	err := decodeOrgError("api:\n  channel: C0API\n  reactions:\n    bogus: x\n")

	require.Error(t, err)
}

func TestRepoConfig_ChannelsList(t *testing.T) {
	org := decodeOrg(t, `
api:
  channels:
    - channel: C0API1
      mentions: ["<@U0ALICE>"]
    - channel: C0API2
`)

	api := org["api"]
	require.Len(t, api.Channels, 2)
	assert.Equal(t, "C0API1", api.Channels[0].Channel)
	assert.True(t, api.Channels[0].MentionsPresent)
	assert.Equal(t, []string{"<@U0ALICE>"}, api.Channels[0].Mentions)
	assert.Equal(t, "C0API2", api.Channels[1].Channel)
	assert.False(t, api.Channels[1].MentionsPresent)
}

func TestRepoConfig_ChannelsRejectsMixWithChannel(t *testing.T) {
	err := decodeOrgError("api:\n  channel: C0BASE\n  channels:\n    - channel: C0API1\n")

	require.Error(t, err)
}

func TestRepoConfig_ChannelsRejectsMentionsSibling(t *testing.T) {
	err := decodeOrgError("api:\n  mentions: [\"<@U0ALICE>\"]\n  channels:\n    - channel: C0API1\n")

	require.Error(t, err)
}

func TestRepoConfig_ChannelsRejectsDuplicate(t *testing.T) {
	err := decodeOrgError("api:\n  channels:\n    - channel: C0DUP\n    - channel: C0DUP\n")

	require.Error(t, err)
}

func TestChannelSpec_RejectsMissingChannel(t *testing.T) {
	err := decodeOrgError("api:\n  channels:\n    - mentions: [\"<@U0ALICE>\"]\n")

	require.Error(t, err)
}

func TestRepoConfig_ChannelsRejectsEmptyList(t *testing.T) {
	err := decodeOrgError("api:\n  channels: []\n")

	require.Error(t, err)
}

func TestPathRule_ChannelsList(t *testing.T) {
	org := decodeOrg(t, `
monorepo:
  channel: C0BASE
  paths:
    services/pay:
      channels:
        - channel: C0PAY1
        - channel: C0PAY2
          mentions: []
`)

	rule := org["monorepo"].Paths[0]
	assert.Equal(t, "services/pay", rule.Dir)
	require.Len(t, rule.Channels, 2)
	assert.Equal(t, "C0PAY1", rule.Channels[0].Channel)
	assert.Equal(t, "C0PAY2", rule.Channels[1].Channel)
	assert.True(t, rule.Channels[1].MentionsPresent)
	assert.Empty(t, rule.Channels[1].Mentions)
}

func TestPathRule_ChannelsRejectsMixWithChannel(t *testing.T) {
	err := decodeOrgError("monorepo:\n  channel: C0BASE\n  paths:\n    services/pay:\n      channel: C0PAY\n      channels:\n        - channel: C0PAY1\n")

	require.Error(t, err)
}

func TestPathRule_ChannelsRejectsMixWithMentions(t *testing.T) {
	err := decodeOrgError("monorepo:\n  channel: C0BASE\n  paths:\n    services/pay:\n      mentions: [\"<@U0ALICE>\"]\n      channels:\n        - channel: C0PAY1\n")

	require.Error(t, err)
}

func TestRepoConfig_DigestCountryRejected(t *testing.T) {
	// country is global-only for the same reason as timezone: one team calendar
	// per server. Setting it on a tier must fail, not be silently ignored.
	err := decodeOrgError("api:\n  channel: C0API\n  digest:\n    country: US\n")

	require.Error(t, err)
}

func TestRepoConfig_DeleteOnCloseOverride(t *testing.T) {
	org := decodeOrg(t, "api:\n  channel: C0API\n  cleanup:\n    delete_on_close: true\n")

	api := org["api"]
	require.NotNil(t, api.DeleteOnClose)
	assert.True(t, *api.DeleteOnClose)
}

func TestRepoConfig_DeleteOnCloseAbsentMeansNil(t *testing.T) {
	api := decodeOrg(t, "api:\n  channel: C0API\n")["api"]

	assert.Nil(t, api.DeleteOnClose, "an absent cleanup block leaves the tier inheriting")
}

func TestRepoConfig_UnknownCleanupKeyRejected(t *testing.T) {
	err := decodeOrgError("api:\n  channel: C0API\n  cleanup:\n    bogus: x\n")

	require.Error(t, err)
}

func TestRepoConfig_CleanupMessageTTLRejected(t *testing.T) {
	// message_ttl_days is a global-only knob (one sweep for the whole database);
	// setting it on a per-repo tier must fail rather than be silently ignored.
	err := decodeOrgError("api:\n  channel: C0API\n  cleanup:\n    message_ttl_days: 7\n")

	require.Error(t, err)
}

func TestRepoConfig_NotificationsAbsentMeansNil(t *testing.T) {
	api := decodeOrg(t, "api:\n  channel: C0API\n")["api"]

	assert.Nil(t, api.Notifications, "an absent notifications block leaves the tier inheriting")
}

func TestRepoConfig_Notifications(t *testing.T) {
	testCases := []struct {
		name  string
		block string
		want  domain.NotificationsConfig
	}{
		{
			name:  "per-event",
			block: "    strategy: per-event\n",
			want:  domain.NotificationsConfig{Strategy: domain.StrategyPerEvent},
		},
		{
			name:  "hourly",
			block: "    strategy: scheduled\n    mode: hourly\n",
			want:  domain.NotificationsConfig{Strategy: domain.StrategyScheduled, Mode: domain.ScheduleModeHourly},
		},
		{
			name:  "at",
			block: "    strategy: scheduled\n    mode: at\n    at: [\"10:30\", \"9:05\"]\n",
			want: domain.NotificationsConfig{
				Strategy: domain.StrategyScheduled,
				Mode:     domain.ScheduleModeAt,
				At:       []domain.TimeOfDay{{Hour: 10, Minute: 30}, {Hour: 9, Minute: 5}},
			},
		},
		{
			name:  "cron",
			block: "    strategy: scheduled\n    mode: cron\n    cron: [\"0 9 * * 1-5\", \"0 14 * * 1-5\"]\n",
			want: domain.NotificationsConfig{
				Strategy: domain.StrategyScheduled,
				Mode:     domain.ScheduleModeCron,
				Cron:     []string{"0 9 * * 1-5", "0 14 * * 1-5"},
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := decodeOrg(t, "api:\n  channel: C0API\n  notifications:\n"+testCase.block)["api"]

			require.NotNil(t, api.Notifications)
			assert.Equal(t, testCase.want, *api.Notifications)
		})
	}
}

func TestRepoConfig_NotificationsRejected(t *testing.T) {
	testCases := []struct {
		name    string
		block   string
		wantErr string
	}{
		{name: "missing strategy", block: "    mode: hourly\n", wantErr: "strategy is required"},
		{name: "unknown strategy", block: "    strategy: batched\n", wantErr: `unknown strategy "batched"`},
		{name: "per-event with mode", block: "    strategy: per-event\n    mode: hourly\n", wantErr: "only valid with strategy: scheduled"},
		{name: "scheduled without mode", block: "    strategy: scheduled\n", wantErr: "mode is required"},
		{name: "unknown mode", block: "    strategy: scheduled\n    mode: daily\n", wantErr: `unknown mode "daily"`},
		{name: "hourly with at", block: "    strategy: scheduled\n    mode: hourly\n    at: [\"10:00\"]\n", wantErr: "at is only valid with mode: at"},
		{name: "hourly with cron", block: "    strategy: scheduled\n    mode: hourly\n    cron: [\"0 9 * * *\"]\n", wantErr: "cron is only valid with mode: cron"},
		{name: "at without times", block: "    strategy: scheduled\n    mode: at\n", wantErr: "mode: at needs at times"},
		{name: "at empty list", block: "    strategy: scheduled\n    mode: at\n    at: []\n", wantErr: "mode: at needs at times"},
		{name: "at not a list", block: "    strategy: scheduled\n    mode: at\n    at: \"10:30\"\n", wantErr: "at: must be a list"},
		{name: "at bad time", block: "    strategy: scheduled\n    mode: at\n    at: [\"25:00\"]\n", wantErr: `at: "25:00"`},
		{name: "at with cron", block: "    strategy: scheduled\n    mode: at\n    at: [\"10:00\"]\n    cron: [\"0 9 * * *\"]\n", wantErr: "cron is only valid with mode: cron"},
		{name: "cron without specs", block: "    strategy: scheduled\n    mode: cron\n    cron: []\n", wantErr: "mode: cron needs cron specs"},
		{name: "cron bad spec", block: "    strategy: scheduled\n    mode: cron\n    cron: [\"every morning\"]\n", wantErr: `cron: "every morning"`},
		{name: "unknown key", block: "    strategy: scheduled\n    mode: hourly\n    timezone: UTC\n", wantErr: `unknown field "timezone"`},
		{name: "duplicate key", block: "    strategy: scheduled\n    strategy: per-event\n", wantErr: `duplicate key "strategy"`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := decodeOrgError("api:\n  channel: C0API\n  notifications:\n" + testCase.block)

			require.ErrorContains(t, err, testCase.wantErr)
		})
	}
}
