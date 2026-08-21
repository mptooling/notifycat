package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mptooling/notifycat/internal/platform/config"
)

// writeWireConfig writes doc as the config file and points Load at it.
func writeWireConfig(t *testing.T, doc string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIFYCAT_CONFIG_FILE", path)
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-x")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "shh")
}

func TestLoad_PerTierBehavioralBlocks(t *testing.T) {
	writeWireConfig(t, `
git_provider: github
mappings:
  acme:
    api:
      channel: C0123456789
      mentions: ["<@U1>"]
      reviews:
        ignore_ai_reviews: true
      reactions:
        new_pr: rocket
      paths:
        services/payments:
          channel: C0123456780
`)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v; per-tier behavioral blocks must parse", err)
	}
	tier := cfg.Mappings["acme"]["api"]
	if tier.IgnoreAIReviews == nil || !*tier.IgnoreAIReviews {
		t.Error("reviews.ignore_ai_reviews override lost")
	}
	if tier.Reactions == nil || tier.Reactions.NewPR == nil || *tier.Reactions.NewPR != "rocket" {
		t.Error("reactions.new_pr override lost")
	}
	if len(tier.Paths) != 1 || tier.Paths[0].Dir != "services/payments" {
		t.Errorf("paths block lost: %+v", tier.Paths)
	}
	if !tier.MentionsPresent || !reflect.DeepEqual(tier.Mentions, []string{"<@U1>"}) {
		t.Errorf("mentions tri-state lost: present=%v mentions=%v", tier.MentionsPresent, tier.Mentions)
	}
}

func TestLoad_MentionsEmptyListMeansNobody(t *testing.T) {
	writeWireConfig(t, `
git_provider: github
mappings:
  acme:
    api:
      channel: C0123456789
      mentions: []
`)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	tier := cfg.Mappings["acme"]["api"]
	if !tier.MentionsPresent || len(tier.Mentions) != 0 {
		t.Errorf("explicit empty mentions must decode as present+empty; present=%v mentions=%v", tier.MentionsPresent, tier.Mentions)
	}
}

func TestLoad_DigestWithoutEnabledStaysEnabled(t *testing.T) {
	writeWireConfig(t, `
git_provider: github
digest:
  schedule: "0 8 * * *"
`)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Digest == nil || !cfg.Digest.Enabled {
		t.Errorf("digest without explicit enabled must stay enabled; got %+v", cfg.Digest)
	}
	if cfg.Digest.Schedule != "0 8 * * *" {
		t.Errorf("Schedule = %q", cfg.Digest.Schedule)
	}
}

func TestLoad_UnknownTierKeyRejected(t *testing.T) {
	writeWireConfig(t, `
git_provider: github
mappings:
  acme:
    api:
      channel: C0123456789
      typo_key: true
`)
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() succeeded with an unknown tier key; want error")
	}
}

func TestLoad_MentionsNullRejected(t *testing.T) {
	writeWireConfig(t, `
git_provider: github
mappings:
  acme:
    api:
      channel: C0123456789
      mentions: null
`)
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() succeeded with mentions: null; want error (omit the key or use [])")
	}
}
