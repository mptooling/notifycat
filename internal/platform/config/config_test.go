package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/platform/config"
)

const minimalConfig = "git_provider: github\nserver:\n  log_level: info\n"

// writeConfig writes a config.yaml into a temp dir, points NOTIFYCAT_CONFIG_FILE
// at it, and clears every secret + retired env var so each test starts clean.
func writeConfig(t *testing.T, body string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	t.Setenv("NOTIFYCAT_CONFIG_FILE", path)
	for _, name := range []string{
		"ADDR", "LOG_LEVEL", "LOG_FORMAT", "DATABASE_URL", "NOTIFYCAT_MAPPINGS_FILE",
		"SLACK_BASE_URL", "GITHUB_BASE_URL", "NOTIFYCAT_MESSAGE_TTL_DAYS",
		"NOTIFYCAT_IGNORE_AI_REVIEWS", "NOTIFYCAT_DEPENDABOT_FORMAT",
		"SLACK_REACTIONS_ENABLED", "SLACK_REACTION_NEW_PR",
		"GITHUB_WEBHOOK_SECRET", "SLACK_BOT_TOKEN", "GITHUB_TOKEN",
		"SLACK_SIGNING_SECRET",
	} {
		t.Setenv(name, "")
	}
}

func setSecrets(t *testing.T) {
	t.Helper()

	t.Setenv("GITHUB_WEBHOOK_SECRET", "shh")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-x")
}

// loadConfigured writes body, applies the github secrets, and loads.
func loadConfigured(t *testing.T, body string) (config.Config, error) {
	t.Helper()

	writeConfig(t, body)
	setSecrets(t)
	return config.Load()
}

func TestLoad_RequiresGitProvider(t *testing.T) {
	_, err := loadConfigured(t, "server:\n  log_level: info\n")

	require.Error(t, err)
	assert.ErrorContains(t, err, "git_provider")
	assert.ErrorContains(t, err, "upgrading.md", "the error points at the upgrade doc")
}

func TestLoad_GitProviderGitHub_Boots(t *testing.T) {
	cfg, err := loadConfigured(t, minimalConfig)

	require.NoError(t, err)
	assert.Equal(t, kernel.ProviderGitHub, cfg.GitProvider)
}

func TestLoad_GitProviderBitbucket_Boots(t *testing.T) {
	writeConfig(t, "git_provider: bitbucket\nserver:\n  log_level: info\n")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-x")
	t.Setenv("BITBUCKET_WEBHOOK_SECRET", "bb-shh")

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, kernel.ProviderBitbucket, cfg.GitProvider)
}

func TestLoad_BitbucketRequiresWebhookSecret(t *testing.T) {
	writeConfig(t, "git_provider: bitbucket\nserver:\n  log_level: info\n")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-x")

	_, err := config.Load()

	var missing *config.MissingVarError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "BITBUCKET_WEBHOOK_SECRET", missing.Var)
}

func TestLoad_GitHubProviderDoesNotRequireBitbucketSecret(t *testing.T) {
	_, err := loadConfigured(t, minimalConfig)

	assert.NoError(t, err, "a github deployment never needs the bitbucket secret")
}

func TestLoad_RejectsInvalidProvider(t *testing.T) {
	_, err := loadConfigured(t, "git_provider: gitlab\n")

	require.Error(t, err)
	require.ErrorContains(t, err, "git_provider")
	assert.ErrorContains(t, err, "invalid")
}

func TestLoad_RequiresWebhookSecret(t *testing.T) {
	writeConfig(t, minimalConfig)
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-x")

	_, err := config.Load()

	var missing *config.MissingVarError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "GITHUB_WEBHOOK_SECRET", missing.Var)
}

func TestLoad_RequiresSlackBotToken(t *testing.T) {
	writeConfig(t, minimalConfig)
	t.Setenv("GITHUB_WEBHOOK_SECRET", "shh")

	_, err := config.Load()

	var missing *config.MissingVarError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "SLACK_BOT_TOKEN", missing.Var)
}

func TestLoad_SlackSigningSecretIsOptional(t *testing.T) {
	cfg, err := loadConfigured(t, minimalConfig)

	require.NoError(t, err)
	assert.Empty(t, cfg.SlackSigningSecret.Reveal())
}

func TestLoad_SlackSigningSecretRead(t *testing.T) {
	writeConfig(t, minimalConfig)
	setSecrets(t)
	t.Setenv("SLACK_SIGNING_SECRET", "v0-signing")

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "v0-signing", cfg.SlackSigningSecret.Reveal())
}

func TestLoad_AppliesDefaultsForAbsentKeys(t *testing.T) {
	cfg, err := loadConfigured(t, minimalConfig)

	require.NoError(t, err)
	assert.Equal(t, ":8080", cfg.Addr)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "text", cfg.LogFormat)
	assert.Equal(t, "file:./data/notifycat.db", cfg.DatabaseURL)
	assert.Equal(t, "https://slack.com", cfg.SlackBaseURL)
	assert.Equal(t, "https://api.github.com", cfg.GitHubBaseURL)
	assert.True(t, cfg.Reactions.Enabled)
	assert.Equal(t, "eyes", cfg.Reactions.NewPR)
	assert.Equal(t, "twisted_rightwards_arrows", cfg.Reactions.MergedPR)
	assert.Equal(t, "x", cfg.Reactions.ClosedPR)
	assert.Equal(t, "white_check_mark", cfg.Reactions.Approved)
	assert.Equal(t, "speech_balloon", cfg.Reactions.Commented)
	assert.Equal(t, "exclamation", cfg.Reactions.RequestChange)
	assert.Equal(t, "robot_face", cfg.Reactions.BotReview)
	assert.Equal(t, 30, cfg.MessageTTLDays)
	assert.False(t, cfg.IgnoreAIReviews)
	assert.True(t, cfg.DependabotFormat)
}

func TestLoad_OverridesAndMappings(t *testing.T) {
	cfg, err := loadConfigured(t, `
git_provider: github
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
    web:
      channel: C0123ABCDE
`)

	require.NoError(t, err)
	assert.Equal(t, ":9000", cfg.Addr)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "json", cfg.LogFormat)
	assert.Equal(t, "notifycat.example.com", cfg.Domain)
	assert.Equal(t, "file:/tmp/custom.db", cfg.DatabaseURL)
	assert.False(t, cfg.Reactions.Enabled)
	assert.Equal(t, "rocket", cfg.Reactions.NewPR)
	assert.Equal(t, "twisted_rightwards_arrows", cfg.Reactions.MergedPR, "un-overridden reaction defaults survive")
	assert.True(t, cfg.IgnoreAIReviews)
	assert.False(t, cfg.DependabotFormat)
	assert.Equal(t, 7, cfg.MessageTTLDays)
	require.NotNil(t, cfg.Digest)
	assert.False(t, cfg.Digest.Enabled)
	require.Contains(t, cfg.Mappings, "acme")
	assert.Equal(t, "C0123ABCDE", cfg.Mappings["acme"]["web"].Channel)
}

func TestLoad_DigestTimezone_DefaultsToUTC(t *testing.T) {
	cfg, err := loadConfigured(t, minimalConfig)

	require.NoError(t, err)
	require.NotNil(t, cfg.DigestTimezone)
	assert.Equal(t, "UTC", cfg.DigestTimezone.String())
}

func TestLoad_DigestTimezone_Valid(t *testing.T) {
	cfg, err := loadConfigured(t, "git_provider: github\ndigest:\n  timezone: \"Europe/Kyiv\"\n")

	require.NoError(t, err)
	require.NotNil(t, cfg.DigestTimezone)
	assert.Equal(t, "Europe/Kyiv", cfg.DigestTimezone.String())
}

func TestLoad_DigestTimezone_InvalidRejected(t *testing.T) {
	_, err := loadConfigured(t, "git_provider: github\ndigest:\n  timezone: \"Mars/Phobos\"\n")

	assert.Error(t, err)
}

func TestLoad_RejectsUnknownTierKey(t *testing.T) {
	_, err := loadConfigured(t, "mappings:\n  acme:\n    api:\n      channel: C0API\n      bogus: x\n")

	assert.Error(t, err)
}

func TestLoad_MessageTTLDays_RejectsZero(t *testing.T) {
	_, err := loadConfigured(t, "git_provider: github\ncleanup:\n  message_ttl_days: 0\n")

	assert.Error(t, err)
}

func TestLoad_MissingFileIsError(t *testing.T) {
	writeConfig(t, minimalConfig)
	setSecrets(t)
	t.Setenv("NOTIFYCAT_CONFIG_FILE", filepath.Join(t.TempDir(), "does-not-exist.yaml"))

	_, err := config.Load()

	assert.Error(t, err)
}

func TestLoad_UnknownKeyRejected(t *testing.T) {
	_, err := loadConfigured(t, "server:\n  not_a_real_key: x\n")

	assert.Error(t, err)
}

func TestLoad_MappingsTierWithNoChannel_Rejected(t *testing.T) {
	_, err := loadConfigured(t, `
git_provider: github
mappings:
  acme:
    api:
      mentions: ["<@U1>"]
`)

	assert.Error(t, err, "api has no channel and no org/* to inherit from")
}

func TestLoad_EmptyOrg_Rejected(t *testing.T) {
	_, err := loadConfigured(t, "git_provider: github\nmappings:\n  acme: {}\n")

	assert.Error(t, err)
}

func TestLoad_EmptyMappings_Valid(t *testing.T) {
	_, err := loadConfigured(t, "git_provider: github\nmappings: {}\n")

	assert.NoError(t, err)
}

func TestLoad_RetiredEnvVarRejected(t *testing.T) {
	writeConfig(t, minimalConfig)
	setSecrets(t)
	t.Setenv("LOG_LEVEL", "debug")

	_, err := config.Load()

	assert.Error(t, err, "app config lives in config.yaml now; a retired env var must point at the migration")
}

func TestLoad_SecretsAreSecretType(t *testing.T) {
	cfg, err := loadConfigured(t, minimalConfig)

	require.NoError(t, err)
	assert.Equal(t, "shh", cfg.GitHubWebhookSecret.Reveal())
	assert.Equal(t, "xoxb-x", cfg.SlackBotToken.Reveal())
	assert.NotEqual(t, "shh", cfg.GitHubWebhookSecret.String(), "String() must redact")
}
