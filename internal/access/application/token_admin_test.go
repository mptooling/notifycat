package application_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/access/application"
	"github.com/mptooling/notifycat/internal/access/domain"
)

func newAdminFixture() (*application.TokenAdmin, *fakeTokenStore, *bytes.Buffer) {
	logger, logged := captureLogger()
	tokens := newFakeTokenStore()
	tokens.tokens[tokenID] = domain.StoredToken{Token: domain.Token{ID: tokenID, User: "alice", CreatedAt: now}}
	return application.NewTokenAdmin(tokens, logger), tokens, logged
}

func TestTokenAdmin_List_ReturnsStoreTokens(t *testing.T) {
	admin, _, _ := newAdminFixture()

	tokens, err := admin.List(t.Context())

	require.NoError(t, err)
	assert.Equal(t, []domain.Token{{ID: tokenID, User: "alice", CreatedAt: now}}, tokens)
}

func TestTokenAdmin_Delete_RemovesAndLogs(t *testing.T) {
	admin, tokens, logged := newAdminFixture()

	err := admin.Delete(t.Context(), tokenID)

	require.NoError(t, err)
	assert.Equal(t, []string{tokenID}, tokens.deleted)
	assert.Contains(t, logged.String(), "agent token deleted")
	assert.Contains(t, logged.String(), "user=alice")
}

func TestTokenAdmin_Delete_UnknownIsErrTokenNotFound(t *testing.T) {
	admin, tokens, _ := newAdminFixture()

	err := admin.Delete(t.Context(), "unknown")

	assert.ErrorIs(t, err, domain.ErrTokenNotFound)
	assert.Empty(t, tokens.deleted)
}
