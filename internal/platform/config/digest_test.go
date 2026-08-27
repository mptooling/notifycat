package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/config"
)

func TestLoad_DigestPresentWithoutEnabled_DefaultsOff(t *testing.T) {
	writeConfig(t, `
git_provider: github
digest:
  schedule: "0 9 * * 1-5"
`)
	setSecrets(t)

	cfg, err := config.Load()

	require.NoError(t, err)
	require.NotNil(t, cfg.Digest)
	assert.False(t, cfg.Digest.Enabled, "the digest is opt-in: a block without `enabled: true` stays off")
}

func TestLoad_DigestExplicitlyDisabled(t *testing.T) {
	writeConfig(t, `
git_provider: github
digest:
  enabled: false
`)
	setSecrets(t)

	cfg, err := config.Load()

	require.NoError(t, err)
	require.NotNil(t, cfg.Digest)
	assert.False(t, cfg.Digest.Enabled, "an explicit `enabled: false` stays off")
}

func TestLoad_DigestRejectsUnknownKey(t *testing.T) {
	writeConfig(t, "git_provider: github\ndigest:\n  bogus: x\n")
	setSecrets(t)

	_, err := config.Load()

	require.Error(t, err, "an unknown digest key must fail fast")
}
