package infrastructure_test

import (
	"path/filepath"
	"testing"

	"github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/config"
)

func TestNewConfigSnapshot_DatabaseOpenable(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "doctor.db")
	cfg := config.Config{
		ConfigFile:          "./config.yaml",
		DatabaseURL:         dsn,
		MessageTTLDays:      30,
		GitHubWebhookSecret: config.Secret("topsecret-wh"),
		SlackBotToken:       config.Secret("xoxb-secret-token"),
	}

	snap := infrastructure.NewConfigSnapshot(cfg, nil, false)

	if !snap.DatabaseOpenable {
		t.Fatalf("DatabaseOpenable = false for writable path; detail: %s", snap.DatabaseDetail)
	}
	if snap.DatabaseDetail != dsn {
		t.Errorf("DatabaseDetail = %q; want %q", snap.DatabaseDetail, dsn)
	}
}

func TestNewConfigSnapshot_DatabaseUnreachablePath(t *testing.T) {
	dsn := "file:/this/path/does/not/exist/doctor.db"
	cfg := config.Config{
		ConfigFile:          "./config.yaml",
		DatabaseURL:         dsn,
		MessageTTLDays:      30,
		GitHubWebhookSecret: config.Secret("topsecret-wh"),
		SlackBotToken:       config.Secret("xoxb-secret-token"),
	}

	snap := infrastructure.NewConfigSnapshot(cfg, nil, false)

	if snap.DatabaseOpenable {
		t.Fatalf("DatabaseOpenable = true for unreachable DSN")
	}
	if snap.DatabaseDetail == "" {
		t.Error("DatabaseDetail is empty; want an error message")
	}
}

func TestNewConfigSnapshot_EmptyDSN(t *testing.T) {
	cfg := config.Config{
		ConfigFile:          "./config.yaml",
		DatabaseURL:         "",
		MessageTTLDays:      30,
		GitHubWebhookSecret: config.Secret("topsecret-wh"),
		SlackBotToken:       config.Secret("xoxb-secret-token"),
	}

	snap := infrastructure.NewConfigSnapshot(cfg, nil, false)

	if snap.DatabaseOpenable {
		t.Fatalf("DatabaseOpenable = true for empty DSN")
	}
}

func TestNewConfigSnapshot_SecretBooleans(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "secrets.db")
	cfg := config.Config{
		ConfigFile:          "./config.yaml",
		DatabaseURL:         dsn,
		MessageTTLDays:      30,
		GitHubWebhookSecret: config.Secret("topsecret-wh"),
		SlackBotToken:       config.Secret("xoxb-secret-token"),
		GitHubToken:         config.Secret("ghp-some-token"),
	}

	snap := infrastructure.NewConfigSnapshot(cfg, nil, false)

	if !snap.WebhookSecretSet {
		t.Error("WebhookSecretSet = false; want true")
	}
	if !snap.SlackTokenSet {
		t.Error("SlackTokenSet = false; want true")
	}
	if !snap.TokenSet {
		t.Error("TokenSet = false; want true")
	}

	// Raw secret values must never appear in the snapshot.
	if snap.DatabaseDetail == "topsecret-wh" || snap.DatabaseDetail == "xoxb-secret-token" {
		t.Errorf("secret value leaked into DatabaseDetail: %q", snap.DatabaseDetail)
	}
}
