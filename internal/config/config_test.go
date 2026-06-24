package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mptooling/notifycat/internal/config"
)

// writeConfig writes a config.yaml into a temp dir, points NOTIFYCAT_CONFIG_FILE
// at it, and clears every secret + retired env var so each test starts clean.
func writeConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("NOTIFYCAT_CONFIG_FILE", path)
	for _, k := range []string{
		"ADDR", "LOG_LEVEL", "LOG_FORMAT", "DATABASE_URL", "NOTIFYCAT_MAPPINGS_FILE",
		"SLACK_BASE_URL", "GITHUB_BASE_URL", "NOTIFYCAT_MESSAGE_TTL_DAYS",
		"NOTIFYCAT_IGNORE_AI_REVIEWS", "NOTIFYCAT_DEPENDABOT_FORMAT",
		"SLACK_REACTIONS_ENABLED", "SLACK_REACTION_NEW_PR",
		"GITHUB_WEBHOOK_SECRET", "SLACK_BOT_TOKEN", "GITHUB_TOKEN",
	} {
		t.Setenv(k, "")
	}
}

const minimalConfig = "server:\n  log_level: info\n"

func setSecrets(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_WEBHOOK_SECRET", "shh")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-x")
}

func TestLoad_RequiresWebhookSecret(t *testing.T) {
	writeConfig(t, minimalConfig)
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-x")

	_, err := config.Load()
	var missing *config.MissingVarError
	if !errors.As(err, &missing) || missing.Var != "GITHUB_WEBHOOK_SECRET" {
		t.Fatalf("Load() error = %v; want MissingVarError(GITHUB_WEBHOOK_SECRET)", err)
	}
}

func TestLoad_RequiresSlackBotToken(t *testing.T) {
	writeConfig(t, minimalConfig)
	t.Setenv("GITHUB_WEBHOOK_SECRET", "shh")

	_, err := config.Load()
	var missing *config.MissingVarError
	if !errors.As(err, &missing) || missing.Var != "SLACK_BOT_TOKEN" {
		t.Fatalf("Load() error = %v; want MissingVarError(SLACK_BOT_TOKEN)", err)
	}
}

func TestLoad_AppliesDefaultsForAbsentKeys(t *testing.T) {
	writeConfig(t, minimalConfig)
	setSecrets(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Addr", cfg.Addr, ":8080"},
		{"LogLevel", cfg.LogLevel, "info"},
		{"LogFormat", cfg.LogFormat, "text"},
		{"DatabaseURL", cfg.DatabaseURL, "file:./data/notifycat.db"},
		{"SlackBaseURL", cfg.SlackBaseURL, "https://slack.com"},
		{"GitHubBaseURL", cfg.GitHubBaseURL, "https://api.github.com"},
		{"Reactions.Enabled", cfg.Reactions.Enabled, true},
		{"Reactions.NewPR", cfg.Reactions.NewPR, "eyes"},
		{"Reactions.MergedPR", cfg.Reactions.MergedPR, "twisted_rightwards_arrows"},
		{"Reactions.ClosedPR", cfg.Reactions.ClosedPR, "x"},
		{"Reactions.Approved", cfg.Reactions.Approved, "white_check_mark"},
		{"Reactions.Commented", cfg.Reactions.Commented, "speech_balloon"},
		{"Reactions.RequestChange", cfg.Reactions.RequestChange, "exclamation"},
		{"Reactions.BotReview", cfg.Reactions.BotReview, "robot_face"},
		{"MessageTTLDays", cfg.MessageTTLDays, 30},
		{"IgnoreAIReviews", cfg.IgnoreAIReviews, false},
		{"DependabotFormat", cfg.DependabotFormat, true},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v; want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoad_OverridesAndMappings(t *testing.T) {
	writeConfig(t, `
server:
  addr: ":9000"
  log_level: debug
  log_format: json
  domain: notifycat.example.com
database:
  url: "file:/tmp/custom.db"
slack:
  reactions:
    enabled: false
    new_pr: rocket
reviews:
  ignore_ai_reviews: true
  dependabot_format: false
cleanup:
  message_ttl_days: 7
digest:
  enabled: false
mappings:
  acme:
    channel: C0123ABCDE
    repositories:
      - web
`)
	setSecrets(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Addr != ":9000" || cfg.LogLevel != "debug" || cfg.LogFormat != "json" {
		t.Errorf("server overrides not applied: %+v", cfg)
	}
	if cfg.Domain != "notifycat.example.com" {
		t.Errorf("Domain = %q", cfg.Domain)
	}
	if cfg.DatabaseURL != "file:/tmp/custom.db" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.Reactions.Enabled {
		t.Errorf("Reactions.Enabled = true; want false")
	}
	if cfg.Reactions.NewPR != "rocket" {
		t.Errorf("Reactions.NewPR = %q", cfg.Reactions.NewPR)
	}
	if cfg.Reactions.MergedPR != "twisted_rightwards_arrows" {
		t.Errorf("Reactions.MergedPR default lost = %q", cfg.Reactions.MergedPR)
	}
	if !cfg.IgnoreAIReviews || cfg.DependabotFormat {
		t.Errorf("reviews overrides not applied: ignore=%v dependabot=%v", cfg.IgnoreAIReviews, cfg.DependabotFormat)
	}
	if cfg.MessageTTLDays != 7 {
		t.Errorf("MessageTTLDays = %d; want 7", cfg.MessageTTLDays)
	}
	if cfg.Digest == nil || cfg.Digest.Enabled {
		t.Errorf("Digest = %+v; want non-nil, disabled", cfg.Digest)
	}
	if _, ok := cfg.Mappings["acme"]; !ok {
		t.Errorf("Mappings missing acme: %+v", cfg.Mappings)
	}
}

func TestLoad_MessageTTLDays_RejectsZero(t *testing.T) {
	writeConfig(t, "cleanup:\n  message_ttl_days: 0\n")
	setSecrets(t)
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() succeeded with message_ttl_days=0; want error")
	}
}

func TestLoad_MissingFileIsError(t *testing.T) {
	writeConfig(t, minimalConfig)
	setSecrets(t)
	t.Setenv("NOTIFYCAT_CONFIG_FILE", filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() succeeded with missing config.yaml; want error")
	}
}

func TestLoad_UnknownKeyRejected(t *testing.T) {
	writeConfig(t, "server:\n  not_a_real_key: x\n")
	setSecrets(t)
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() succeeded with an unknown key; want error")
	}
}

func TestLoad_RetiredEnvVarRejected(t *testing.T) {
	writeConfig(t, minimalConfig)
	setSecrets(t)
	t.Setenv("LOG_LEVEL", "debug") // retired: app config now lives in config.yaml
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() succeeded with a retired env var set; want error pointing to migration")
	}
}

func TestLoad_SecretsAreSecretType(t *testing.T) {
	writeConfig(t, minimalConfig)
	setSecrets(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.GitHubWebhookSecret.Reveal() != "shh" || cfg.SlackBotToken.Reveal() != "xoxb-x" {
		t.Error("secrets not read from env")
	}
	if cfg.GitHubWebhookSecret.String() == "shh" {
		t.Error("secret renders raw via String()")
	}
}
