package infrastructure_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/config"
)

const (
	webhookSecret = "topsecret-wh"
	slackToken    = "xoxb-secret-token"
)

// snapshotConfig is a minimal, valid operator config pointed at the given DSN.
func snapshotConfig(databaseURL string) config.Config {
	return config.Config{
		ConfigFile:          "./config.yaml",
		DatabaseURL:         databaseURL,
		MessageTTLDays:      30,
		GitHubWebhookSecret: config.Secret(webhookSecret),
		SlackBotToken:       config.Secret(slackToken),
	}
}

func TestNewConfigSnapshot_DatabaseOpenable(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "doctor.db")

	snapshot := infrastructure.NewConfigSnapshot(snapshotConfig(dsn), nil, false)

	require.True(t, snapshot.DatabaseOpenable, "detail: %s", snapshot.DatabaseDetail)
	assert.Equal(t, dsn, snapshot.DatabaseDetail)
}

func TestNewConfigSnapshot_DatabaseUnreachablePath(t *testing.T) {
	snapshot := infrastructure.NewConfigSnapshot(snapshotConfig("file:/this/path/does/not/exist/doctor.db"), nil, false)

	assert.False(t, snapshot.DatabaseOpenable)
	assert.NotEmpty(t, snapshot.DatabaseDetail, "an unopenable database explains why")
}

func TestNewConfigSnapshot_EmptyDSN(t *testing.T) {
	snapshot := infrastructure.NewConfigSnapshot(snapshotConfig(""), nil, false)

	assert.False(t, snapshot.DatabaseOpenable)
}

func TestNewConfigSnapshot_SecretBooleans(t *testing.T) {
	cfg := snapshotConfig("file:" + filepath.Join(t.TempDir(), "secrets.db"))
	cfg.GitHubToken = config.Secret("ghp-some-token")

	snapshot := infrastructure.NewConfigSnapshot(cfg, nil, false)

	assert.True(t, snapshot.WebhookSecretSet)
	assert.True(t, snapshot.SlackTokenSet)
	assert.True(t, snapshot.TokenSet)
	assert.NotContains(t, snapshot.DatabaseDetail, webhookSecret, "a raw secret must never reach the snapshot")
	assert.NotContains(t, snapshot.DatabaseDetail, slackToken)
}
