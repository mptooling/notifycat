package config_test

import (
	"testing"

	"github.com/mptooling/notifycat/internal/platform/config"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

func resolvedMentions(t *testing.T, repository string) []string {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	provider := routingapp.NewProvider(
		routingdomain.Defaults{GitProvider: cfg.GitProvider},
		cfg.Mappings,
		cfg.Digest,
	)
	targets := provider.BaseTargets(repository)
	if len(targets) != 1 {
		t.Fatalf("BaseTargets(%q) = %d targets; want 1", repository, len(targets))
	}
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

	if mentions := resolvedMentions(t, "acme/web"); len(mentions) != 0 {
		t.Errorf("mentions = %v; want empty — `mentions: []` must ping nobody, not @channel", mentions)
	}
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

	mentions := resolvedMentions(t, "acme/web")
	if len(mentions) != 1 || mentions[0] != routingdomain.ChannelMention {
		t.Errorf("mentions = %v; want [%s] — an absent `mentions:` key must default to @channel", mentions, routingdomain.ChannelMention)
	}
}
