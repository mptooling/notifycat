package infrastructure_test

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/access/infrastructure"
)

func TestRandomSecret_NewIDIsSixteenRandomBytesInHex(t *testing.T) {
	source := infrastructure.NewRandomSecret()

	first, err := source.NewID()
	require.NoError(t, err)
	second, err := source.NewID()
	require.NoError(t, err)

	decoded, err := hex.DecodeString(first)
	require.NoError(t, err)
	assert.Len(t, first, 32)
	assert.Len(t, decoded, 16)
	assert.NotEqual(t, first, second)
}

func TestRandomSecret_NewSecretIsThirtyTwoRandomBytesInBase64URL(t *testing.T) {
	source := infrastructure.NewRandomSecret()

	first, err := source.NewSecret()
	require.NoError(t, err)
	second, err := source.NewSecret()
	require.NoError(t, err)

	decoded, err := base64.RawURLEncoding.Strict().DecodeString(first)
	require.NoError(t, err)
	assert.Len(t, first, 43)
	assert.Len(t, decoded, 32)
	assert.NotEqual(t, first, second)
}

func TestRandomSecret_GeneratedCredentialParses(t *testing.T) {
	source := infrastructure.NewRandomSecret()
	id, err := source.NewID()
	require.NoError(t, err)
	secret, err := source.NewSecret()
	require.NoError(t, err)

	_, err = domain.ParseCredential(domain.Credential{Prefix: domain.TokenPrefix, ID: id, Secret: secret}.Encode(), domain.TokenPrefix)

	assert.NoError(t, err)
}

func TestBcryptHasher_HashMatchesOnlyItsSecret(t *testing.T) {
	hasher := infrastructure.NewBcryptHasher()

	hash, err := hasher.Hash("s3cret")
	require.NoError(t, err)
	cost, err := bcrypt.Cost([]byte(hash))
	require.NoError(t, err)

	assert.Equal(t, 10, cost)
	assert.True(t, hasher.Matches(hash, "s3cret"))
	assert.False(t, hasher.Matches(hash, "other"))
	assert.False(t, hasher.Matches("", "s3cret"))
	assert.False(t, hasher.Matches("not-a-hash", "s3cret"))
}

func TestSystemClock_ReturnsUTC(t *testing.T) {
	clock := infrastructure.NewSystemClock()

	assert.Equal(t, time.UTC, clock.Now().Location())
}
