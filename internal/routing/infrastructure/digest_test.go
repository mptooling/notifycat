package infrastructure_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	application "github.com/mptooling/notifycat/internal/routing/application"
	domain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/routing/infrastructure"
)

const digestMappingsTail = `
mappings:
  acme:
    "*":
      channel: C0123ABCDE
`

func loadDigestProvider(t *testing.T, body string) *application.Provider {
	t.Helper()

	provider, err := infrastructure.Load(writeMappingsFile(t, body))
	require.NoError(t, err)
	return provider
}

func TestProvider_Digest_AbsentDefaultsToEnabled(t *testing.T) {
	provider := loadDigestProvider(t, digestMappingsTail)

	digest := provider.Digest()

	assert.True(t, digest.Enabled, "digest is on unless the operator turns it off")
	assert.Equal(t, domain.DefaultDigestSchedule, digest.Schedule)
}

func TestProvider_Digest_CustomSchedule(t *testing.T) {
	provider := loadDigestProvider(t, "digest:\n  schedule: \"0 8 * * 1-5\"\n"+digestMappingsTail)

	digest := provider.Digest()

	assert.True(t, digest.Enabled)
	assert.Equal(t, "0 8 * * 1-5", digest.Schedule)
}

func TestProvider_Digest_ExplicitlyDisabled(t *testing.T) {
	provider := loadDigestProvider(t, "digest:\n  enabled: false\n"+digestMappingsTail)

	digest := provider.Digest()

	assert.False(t, digest.Enabled)
	assert.Equal(t, domain.DefaultDigestSchedule, digest.Schedule, "the schedule still resolves while disabled")
}

func TestProvider_Digest_UnknownFieldRejected(t *testing.T) {
	_, err := infrastructure.Load(writeMappingsFile(t, "digest:\n  frequency: daily\n"+digestMappingsTail))

	require.Error(t, err)
}

func TestProvider_Digest_Timezone(t *testing.T) {
	provider := loadDigestProvider(t, "digest:\n  timezone: \"Europe/Kyiv\"\n"+digestMappingsTail)

	assert.Equal(t, "Europe/Kyiv", provider.Digest().Timezone)
}

func TestProvider_Digest_TimezoneAbsentIsEmpty(t *testing.T) {
	provider := loadDigestProvider(t, digestMappingsTail)

	assert.Empty(t, provider.Digest().Timezone, "config resolves the empty timezone to UTC")
}

func TestProvider_Digest_Country(t *testing.T) {
	provider := loadDigestProvider(t, "digest:\n  country: \"DE\"\n"+digestMappingsTail)

	assert.Equal(t, "DE", provider.Digest().Country)
}

func TestProvider_Digest_CountryAbsentIsEmpty(t *testing.T) {
	provider := loadDigestProvider(t, digestMappingsTail)

	assert.Empty(t, provider.Digest().Country, "no country means weekends-only")
}

func TestProvider_DigestFor_KeepsGlobalCountry(t *testing.T) {
	provider := loadDigestProvider(t, "digest:\n  country: \"DE\"\n"+digestMappingsTail)

	assert.Equal(t, "DE", provider.DigestFor("acme/api").Country, "a repo tier override must not clobber the global country")
}
