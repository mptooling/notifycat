package application_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/access/application"
	"github.com/mptooling/notifycat/internal/access/domain"
)

func TestAdminAuthenticator_AcceptsTheKey(t *testing.T) {
	logger, _ := captureLogger()
	authenticator := application.NewAdminAuthenticator(domain.Settings{AdminKeyHash: "hash:nca_right"}, &fakeHasher{}, logger)

	err := authenticator.Authenticate(t.Context(), "nca_right")

	require.NoError(t, err)
}

func TestAdminAuthenticator_RejectsWrongKey(t *testing.T) {
	logger, logged := captureLogger()
	authenticator := application.NewAdminAuthenticator(domain.Settings{AdminKeyHash: "hash:nca_right"}, &fakeHasher{}, logger)

	err := authenticator.Authenticate(t.Context(), "nca_wrong")

	assert.ErrorIs(t, err, domain.ErrUnauthorized)
	assert.Contains(t, logged.String(), "admin auth failed")
	assert.NotContains(t, logged.String(), "nca_wrong")
}

func TestAdminAuthenticator_EmptyKeySkipsBcrypt(t *testing.T) {
	logger, _ := captureLogger()
	hasher := &fakeHasher{}
	authenticator := application.NewAdminAuthenticator(domain.Settings{AdminKeyHash: "hash:nca_right"}, hasher, logger)

	err := authenticator.Authenticate(t.Context(), "")

	assert.ErrorIs(t, err, domain.ErrUnauthorized)
	assert.Zero(t, hasher.matchCalls)
}
