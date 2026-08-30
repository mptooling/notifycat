package infrastructure_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/routing/application"
	domain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/routing/infrastructure"
)

// providerFromDoc parses a mappings document and wraps it in a Provider so the
// resolution path can be exercised end to end.
func providerFromDoc(t *testing.T, body string) *application.Provider {
	t.Helper()

	file, err := infrastructure.Parse(strings.NewReader(body))
	require.NoError(t, err)
	return application.NewProvider(domain.Defaults{}, file.Mappings, nil)
}

// monorepoDoc is a six-path tier used by most resolution tests:
//
//	modules/acme     → @U0A (channel inherits base)
//	modules/betta    → @U0B (channel inherits base)
//	config           → @U0A, @U0B
//	src/AuthBundle   → own channel C0AUTH00000, @U0AUTH
//	vendor           → silent ([])
//	docs             → inherits base mentions (no key)
const monorepoDoc = "mappings:\n" +
	"  acme:\n" +
	"    the-monorepo:\n" +
	"      channel: C0BASE00000\n" +
	"      mentions: [\"<!subteam^S0ENG>\"]\n" +
	"      paths:\n" +
	"        \"/modules/acme\": {mentions: [\"<@U0A>\"]}\n" +
	"        \"/modules/betta\": {mentions: [\"<@U0B>\"]}\n" +
	"        \"/config\": {mentions: [\"<@U0A>\", \"<@U0B>\"]}\n" +
	"        \"/src/AuthBundle\": {channel: C0AUTH00000, mentions: [\"<@U0AUTH>\"]}\n" +
	"        \"/vendor\": {mentions: []}\n" +
	"        \"/docs\": {}\n"

const plainRepoDoc = "mappings:\n  acme:\n    plain:\n      channel: C0PLAIN0000\n"

func TestTargetsForFiles_FanOutPerChannel(t *testing.T) {
	provider := providerFromDoc(t, monorepoDoc)

	got := provider.TargetsForFiles("acme/the-monorepo", []string{"modules/acme/x.go", "src/AuthBundle/y.go"})

	assert.ElementsMatch(t, []domain.Target{
		{Channel: "C0BASE00000", Mentions: []string{"<@U0A>"}},
		{Channel: "C0AUTH00000", Mentions: []string{"<@U0AUTH>"}},
	}, got, "modules/acme inherits the base channel, src/AuthBundle brings its own")
}

func TestTargetsForFiles_MentionsUnionWithinChannel(t *testing.T) {
	provider := providerFromDoc(t, monorepoDoc)

	got := provider.TargetsForFiles("acme/the-monorepo", []string{"modules/acme/x.go", "config/app.yaml"})

	require.Len(t, got, 1)
	assert.Equal(t, "C0BASE00000", got[0].Channel)
	assert.Equal(t, []string{"<@U0A>", "<@U0B>"}, got[0].Mentions, "two rules on one channel dedupe into a union")
}

func TestTargetsForFiles_NoMatchReturnsBase(t *testing.T) {
	provider := providerFromDoc(t, monorepoDoc)

	got := provider.TargetsForFiles("acme/the-monorepo", []string{"README.md"})

	require.Len(t, got, 1)
	assert.Equal(t, "C0BASE00000", got[0].Channel)
	assert.Equal(t, []string{"<!subteam^S0ENG>"}, got[0].Mentions)
}

func TestHasPathRules(t *testing.T) {
	assert.True(t, providerFromDoc(t, monorepoDoc).HasPathRules())
	assert.False(t, providerFromDoc(t, plainRepoDoc).HasPathRules())
}

func TestRepoHasPathRules(t *testing.T) {
	provider := providerFromDoc(t, monorepoDoc)

	assert.True(t, provider.RepoHasPathRules("acme/the-monorepo"))
	assert.False(t, provider.RepoHasPathRules("acme/other"), "an unmapped repo has no rules")
	assert.False(t, providerFromDoc(t, plainRepoDoc).RepoHasPathRules("acme/plain"))
}

func TestPathChannels_DistinctSorted(t *testing.T) {
	provider := providerFromDoc(t, "mappings:\n  acme:\n    mono:\n      channel: C0BASE00000\n      paths:\n"+
		"        \"/a\": {channel: C0ZZZ00000}\n"+
		"        \"/b\": {channel: C0AAA00000}\n"+
		"        \"/c\": {channel: C0AAA00000}\n"+
		"        \"/d\": {mentions: []}\n")

	got := provider.AdditionalChannels("acme/mono")

	assert.Equal(t, []string{"C0AAA00000", "C0ZZZ00000"}, got, "duplicates collapse, channel-less rules drop out, order is sorted")
	assert.Nil(t, provider.AdditionalChannels("acme/unmapped"))
}

func TestBaseTargets_MultiChannelBaseNoPaths(t *testing.T) {
	provider := providerFromDoc(t, `
mappings:
  acme:
    api:
      channels:
        - channel: C0API1
          mentions: ["<@U0A>"]
        - channel: C0API2
`)

	got := provider.BaseTargets("acme/api")

	require.Len(t, got, 2)
	assert.Equal(t, "C0API1", got[0].Channel)
	assert.Equal(t, []string{"<@U0A>"}, got[0].Mentions)
	assert.Equal(t, "C0API2", got[1].Channel)
	assert.Equal(t, []string{domain.ChannelMention}, got[1].Mentions)
}

func TestTargetsForFiles_PathChannelsListReplacesBase(t *testing.T) {
	provider := providerFromDoc(t, `
mappings:
  acme:
    monorepo:
      channel: C0BASE
      paths:
        services/pay:
          channels:
            - channel: C0PAY1
            - channel: C0PAY2
              mentions: []
`)

	got := provider.TargetsForFiles("acme/monorepo", []string{"services/pay/x.go"})

	require.Len(t, got, 2)
	assert.Equal(t, "C0PAY1", got[0].Channel)
	assert.Equal(t, "C0PAY2", got[1].Channel)
	assert.Empty(t, got[1].Mentions, "explicit [] pings nobody")
}

func TestTargetsForFiles_MultiBaseReturnedWhenNoPathMatch(t *testing.T) {
	provider := providerFromDoc(t, `
mappings:
  acme:
    monorepo:
      channels:
        - channel: C0B1
        - channel: C0B2
      paths:
        services/pay:
          channel: C0PAY
`)

	got := provider.TargetsForFiles("acme/monorepo", []string{"README.md"})

	require.Len(t, got, 2)
	assert.Equal(t, "C0B1", got[0].Channel)
	assert.Equal(t, "C0B2", got[1].Channel)
}

func TestTargetsForFiles_ChannelLessPathInheritsPrimary(t *testing.T) {
	provider := providerFromDoc(t, `
mappings:
  acme:
    monorepo:
      channels:
        - channel: C0PRIMARY
        - channel: C0SECOND
      paths:
        services/pay:
          mentions: ["<@U0PAY>"]
`)

	got := provider.TargetsForFiles("acme/monorepo", []string{"services/pay/x.go"})

	require.Len(t, got, 1)
	assert.Equal(t, "C0PRIMARY", got[0].Channel, "a channel-less path rule rides the primary base channel")
	assert.Equal(t, []string{"<@U0PAY>"}, got[0].Mentions)
}
