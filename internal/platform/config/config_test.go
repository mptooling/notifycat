package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

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
		"SLACK_SIGNING_SECRET", "NOTIFYCAT_LOCK_FILE", "NOTIFYCAT_ADMIN_KEY_HASH",
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

func TestLoad_LockFileDefaultsBesideConfigFile(t *testing.T) {
	cfg, err := loadConfigured(t, minimalConfig)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(filepath.Dir(cfg.ConfigFile), "config.lock"), cfg.LockFilePath())
}

func TestLoad_LockFileFromEnvironment(t *testing.T) {
	writeConfig(t, minimalConfig)
	setSecrets(t)
	t.Setenv("NOTIFYCAT_LOCK_FILE", "/app/config.lock")

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "/app/config.lock", cfg.LockFilePath())
}

func TestConfig_SlackInteractivityEnabled(t *testing.T) {
	testCases := []struct {
		name          string
		signingSecret config.Secret
		want          bool
	}{
		{name: "off without a signing secret", signingSecret: "", want: false},
		{name: "on with a signing secret", signingSecret: "v0-signing", want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := config.Config{SlackSigningSecret: testCase.signingSecret}

			got := cfg.SlackInteractivityEnabled()

			assert.Equal(t, testCase.want, got)
		})
	}
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

func TestLoad_DeleteOnClose_DefaultsOff(t *testing.T) {
	cfg, err := loadConfigured(t, minimalConfig)

	require.NoError(t, err)
	assert.False(t, cfg.DeleteOnClose, "removing merged messages is opt-in")
}

func TestLoad_DeleteOnClose_Enabled(t *testing.T) {
	cfg, err := loadConfigured(t, "git_provider: github\ncleanup:\n  delete_on_close: true\n")

	require.NoError(t, err)
	assert.True(t, cfg.DeleteOnClose)
}

func adminKeyHash(t *testing.T) string {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte("nca_test"), bcrypt.MinCost)
	require.NoError(t, err)
	return string(hash)
}

func loadWithAdminKeyHash(t *testing.T, body, hashValue string) (config.Config, error) {
	t.Helper()

	writeConfig(t, body)
	setSecrets(t)
	t.Setenv("NOTIFYCAT_ADMIN_KEY_HASH", hashValue)
	return config.Load()
}

func TestLoad_AgentAPI_DefaultsOff(t *testing.T) {
	cfg, err := loadConfigured(t, minimalConfig)

	require.NoError(t, err)
	assert.False(t, cfg.AgentAPIEnabled)
	assert.Empty(t, cfg.AdminAddr)
	assert.Empty(t, cfg.AdminKeyHash.Reveal())
}

func TestLoad_AgentAPI_ReadsKeysAndHash(t *testing.T) {
	hash := adminKeyHash(t)
	body := minimalConfig + "  admin_addr: \"127.0.0.1:9091\"\nagent_api:\n  enabled: true\n"

	cfg, err := loadWithAdminKeyHash(t, body, hash)

	require.NoError(t, err)
	assert.True(t, cfg.AgentAPIEnabled)
	assert.Equal(t, "127.0.0.1:9091", cfg.AdminAddr)
	assert.Equal(t, hash, cfg.AdminKeyHash.Reveal())
	assert.Equal(t, "[REDACTED]", cfg.AdminKeyHash.String())
}

func TestLoad_AdminKeyHash_StripsSingleQuotes(t *testing.T) {
	hash := adminKeyHash(t)
	body := minimalConfig + "  admin_addr: \"127.0.0.1:9091\"\n"

	cfg, err := loadWithAdminKeyHash(t, body, "'"+hash+"'")

	require.NoError(t, err)
	assert.Equal(t, hash, cfg.AdminKeyHash.Reveal())
}

func TestLoad_AdminAddrWithoutKeyHash_Fails(t *testing.T) {
	body := minimalConfig + "  admin_addr: \"127.0.0.1:9091\"\n"

	_, err := loadConfigured(t, body)

	var missing *config.MissingVarError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "NOTIFYCAT_ADMIN_KEY_HASH", missing.Var, "the check runs even while the API is disabled")
}

func TestLoad_AdminAddrEqualsAddr_Fails(t *testing.T) {
	body := "git_provider: github\nserver:\n  addr: \":9091\"\n  admin_addr: \":9091\"\n"

	_, err := loadWithAdminKeyHash(t, body, adminKeyHash(t))

	require.Error(t, err)
	assert.ErrorContains(t, err, "server.admin_addr")
	assert.ErrorContains(t, err, "server.addr")
}

func TestLoad_AdminKeyHashNotBcrypt_Fails(t *testing.T) {
	hash := adminKeyHash(t)
	testCases := []struct {
		name  string
		value string
	}{
		{"plain text", "not-a-hash"},
		{"dollar signs expanded", strings.ReplaceAll(hash, "$", "")},
		{"cut after the cost", "$2a$10$"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			body := minimalConfig + "  admin_addr: \"127.0.0.1:9091\"\n"

			_, err := loadWithAdminKeyHash(t, body, testCase.value)

			require.Error(t, err)
			assert.ErrorContains(t, err, "NOTIFYCAT_ADMIN_KEY_HASH")
			assert.ErrorContains(t, err, "single quotes")
		})
	}
}

func TestLoad_AgentAPI_UnknownKeyRejected(t *testing.T) {
	_, err := loadConfigured(t, minimalConfig+"agent_api:\n  enable: true\n")

	require.Error(t, err)
}
