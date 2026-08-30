package infrastructure

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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
