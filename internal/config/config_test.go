package config_test

import (
	"errors"
	"testing"

	"github.com/mptooling/notifycat/internal/config"
)

// setEnv sets environment variables for the test and restores them on cleanup.
// It works on a per-test basis via t.Setenv which is goroutine-safe and
// scoped to the test.
func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func clearAll(t *testing.T) {
	t.Helper()
	keys := []string{
		"ADDR", "LOG_LEVEL", "LOG_FORMAT", "DATABASE_URL",
		"GITHUB_WEBHOOK_SECRET", "SLACK_BOT_TOKEN",
		"SLACK_REACTIONS_ENABLED",
		"SLACK_REACTION_NEW_PR", "SLACK_REACTION_MERGED_PR",
		"SLACK_REACTION_CLOSED_PR", "SLACK_REACTION_PR_APPROVED",
		"SLACK_REACTION_PR_COMMENTED", "SLACK_REACTION_PR_REQUEST_CHANGE",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
}

func TestLoad_RequiresWebhookSecret(t *testing.T) {
	clearAll(t)
	setEnv(t, map[string]string{"SLACK_BOT_TOKEN": "xoxb-x"})

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() succeeded with missing GITHUB_WEBHOOK_SECRET; want error")
	}
	var missing *config.MissingVarError
	if !errors.As(err, &missing) {
		t.Fatalf("Load() error = %v; want *MissingVarError", err)
	}
	if missing.Var != "GITHUB_WEBHOOK_SECRET" {
		t.Fatalf("MissingVarError.Var = %q; want GITHUB_WEBHOOK_SECRET", missing.Var)
	}
}

func TestLoad_RequiresSlackBotToken(t *testing.T) {
	clearAll(t)
	setEnv(t, map[string]string{"GITHUB_WEBHOOK_SECRET": "shh"})

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() succeeded with missing SLACK_BOT_TOKEN; want error")
	}
	var missing *config.MissingVarError
	if !errors.As(err, &missing) {
		t.Fatalf("Load() error = %v; want *MissingVarError", err)
	}
	if missing.Var != "SLACK_BOT_TOKEN" {
		t.Fatalf("MissingVarError.Var = %q; want SLACK_BOT_TOKEN", missing.Var)
	}
}

func TestLoad_AppliesDefaults(t *testing.T) {
	clearAll(t)
	setEnv(t, map[string]string{
		"GITHUB_WEBHOOK_SECRET": "shh",
		"SLACK_BOT_TOKEN":       "xoxb-x",
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v; want nil", err)
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
		{"Reactions.Enabled", cfg.Reactions.Enabled, true},
		{"Reactions.NewPR", cfg.Reactions.NewPR, "large_green_circle"},
		{"Reactions.MergedPR", cfg.Reactions.MergedPR, "twisted_rightwards_arrows"},
		{"Reactions.ClosedPR", cfg.Reactions.ClosedPR, "x"},
		{"Reactions.Approved", cfg.Reactions.Approved, "white_check_mark"},
		{"Reactions.Commented", cfg.Reactions.Commented, "speech_balloon"},
		{"Reactions.RequestChange", cfg.Reactions.RequestChange, "exclamation"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v; want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoad_OverridesDefaults(t *testing.T) {
	clearAll(t)
	setEnv(t, map[string]string{
		"GITHUB_WEBHOOK_SECRET":   "shh",
		"SLACK_BOT_TOKEN":         "xoxb-x",
		"ADDR":                    ":9000",
		"LOG_LEVEL":               "debug",
		"LOG_FORMAT":              "json",
		"DATABASE_URL":            "file:/tmp/custom.db",
		"SLACK_REACTIONS_ENABLED": "false",
		"SLACK_REACTION_NEW_PR":   "rocket",
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Addr != ":9000" || cfg.LogLevel != "debug" || cfg.LogFormat != "json" {
		t.Errorf("server overrides not applied: %+v", cfg)
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
}

func TestLoad_SecretsAreSecretType(t *testing.T) {
	clearAll(t)
	setEnv(t, map[string]string{
		"GITHUB_WEBHOOK_SECRET": "shh",
		"SLACK_BOT_TOKEN":       "xoxb-x",
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.GitHubWebhookSecret.Reveal() != "shh" {
		t.Errorf("GitHubWebhookSecret.Reveal() = %q", cfg.GitHubWebhookSecret.Reveal())
	}
	if cfg.SlackBotToken.Reveal() != "xoxb-x" {
		t.Errorf("SlackBotToken.Reveal() = %q", cfg.SlackBotToken.Reveal())
	}
	if cfg.GitHubWebhookSecret.String() == "shh" || cfg.SlackBotToken.String() == "xoxb-x" {
		t.Error("secrets render as their raw value via String()")
	}
}
