package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/config"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

func resolvedMentions(t *testing.T, repository string) []string {
	t.Helper()

	cfg, err := config.Load()
	require.NoError(t, err)

	provider := routingapp.NewProvider(
		routingdomain.Defaults{GitProvider: cfg.GitProvider},
		cfg.Mappings,
		cfg.Digest,
	)
	targets := provider.BaseTargets(repository)
	require.Len(t, targets, 1)
	return targets[0].Mentions
}

func TestLoad_EmptyMentionsPingsNobody(t *testing.T) {
	writeConfig(t, `
git_provider: github
mappings:
  acme:
    web:
      channel: C0WEB
      mentions: []
`)
	setSecrets(t)

	assert.Empty(t, resolvedMentions(t, "acme/web"), "`mentions: []` pings nobody, not @channel")
}

func TestLoad_AbsentMentionsDefaultsToChannel(t *testing.T) {
	writeConfig(t, `
git_provider: github
mappings:
  acme:
    web:
      channel: C0WEB
`)
	setSecrets(t)

	assert.Equal(t, []string{routingdomain.ChannelMention}, resolvedMentions(t, "acme/web"), "an absent `mentions:` key defaults to @channel")
}
