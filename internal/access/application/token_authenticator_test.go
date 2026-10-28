package application_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/access/application"
	"github.com/mptooling/notifycat/internal/access/domain"
)

type authenticatorFixture struct {
	authenticator *application.TokenAuthenticator
	tokens        *fakeTokenStore
	hasher        *fakeHasher
	logged        *bytes.Buffer
}

func newAuthenticatorFixture(lastUsedAt *time.Time) authenticatorFixture {
	logger, logged := captureLogger()
	tokens := newFakeTokenStore()
	tokens.tokens[tokenID] = domain.StoredToken{
		Token:      domain.Token{ID: tokenID, User: "alice", CreatedAt: now.Add(-24 * time.Hour), LastUsedAt: lastUsedAt},
		SecretHash: "hash:" + tokenSecret,
	}
	hasher := &fakeHasher{}
	authenticator := application.NewTokenAuthenticator(domain.TokenAuthenticatorParams{
		Tokens: tokens,
		Hasher: hasher,
		Clock:  &fakeClock{now: now},
		Logger: logger,
	})
	return authenticatorFixture{authenticator: authenticator, tokens: tokens, hasher: hasher, logged: logged}
}

func validToken() string { return "nct_" + tokenID + "_" + tokenSecret }

func TestTokenAuthenticator_Authenticate_ReturnsPrincipal(t *testing.T) {
	fixture := newAuthenticatorFixture(nil)

	principal, err := fixture.authenticator.Authenticate(t.Context(), validToken())

	require.NoError(t, err)
	assert.Equal(t, domain.Principal{TokenID: tokenID, User: "alice"}, principal)
}

func TestTokenAuthenticator_Authenticate_Failures(t *testing.T) {
	testCases := []struct {
		name           string
		raw            string
		secret         string
		arrange        func(fixture authenticatorFixture)
		wantReason     domain.AuthFailureReason
		wantMatchCalls int
	}{
		{"malformed", "nct_short", "short", func(authenticatorFixture) {}, domain.AuthFailureMalformed, 0},
		{"invite credential", "nci_" + inviteID + "_" + inviteSecret, inviteSecret, func(authenticatorFixture) {}, domain.AuthFailureMalformed, 0},
		{"unknown", validToken(), tokenSecret, func(fixture authenticatorFixture) { delete(fixture.tokens.tokens, tokenID) }, domain.AuthFailureUnknown, 0},
		{"mismatch", validToken(), tokenSecret, func(fixture authenticatorFixture) {
			stored := fixture.tokens.tokens[tokenID]
			stored.SecretHash = "hash:other"
			fixture.tokens.tokens[tokenID] = stored
		}, domain.AuthFailureMismatch, 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newAuthenticatorFixture(nil)
			testCase.arrange(fixture)

			principal, err := fixture.authenticator.Authenticate(t.Context(), testCase.raw)

			var authErr *domain.AuthError
			require.ErrorAs(t, err, &authErr)
			assert.Equal(t, testCase.wantReason, authErr.Reason)
			assert.ErrorIs(t, err, domain.ErrUnauthorized)
			assert.Zero(t, principal)
			assert.Contains(t, fixture.logged.String(), "agent api auth failed")
			assert.Contains(t, fixture.logged.String(), "reason="+string(testCase.wantReason))
			assert.Equal(t, testCase.wantMatchCalls, fixture.hasher.matchCalls)
			assert.Empty(t, fixture.tokens.touched)
			assert.NotContains(t, fixture.logged.String(), testCase.secret)
			assert.NotContains(t, err.Error(), testCase.secret)
		})
	}
}

func TestTokenAuthenticator_LastUsedWrittenAtMostOncePerMinute(t *testing.T) {
	ago := func(age time.Duration) *time.Time {
		lastUsedAt := now.Add(-age)
		return &lastUsedAt
	}
	testCases := []struct {
		name       string
		lastUsedAt *time.Time
		wantTouch  bool
	}{
		{"never used", nil, true},
		{"30s ago", ago(30 * time.Second), false},
		{"59s ago", ago(59 * time.Second), false},
		{"60s ago", ago(60 * time.Second), true},
		{"2h ago", ago(2 * time.Hour), true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newAuthenticatorFixture(testCase.lastUsedAt)

			_, err := fixture.authenticator.Authenticate(t.Context(), validToken())

			require.NoError(t, err)
			if testCase.wantTouch {
				assert.Equal(t, []touchCall{{id: tokenID, usedAt: now}}, fixture.tokens.touched)
			} else {
				assert.Empty(t, fixture.tokens.touched)
			}
		})
	}
}

func TestTokenAuthenticator_TouchFailureStillAuthenticates(t *testing.T) {
	fixture := newAuthenticatorFixture(nil)
	fixture.tokens.touchErr = errors.New("locked")

	_, err := fixture.authenticator.Authenticate(t.Context(), validToken())

	require.NoError(t, err)
	assert.Contains(t, fixture.logged.String(), "agent token last_used_at not updated")
}

func TestTokenAuthenticator_StoreErrorIsNotUnauthorized(t *testing.T) {
	fixture := newAuthenticatorFixture(nil)
	fixture.tokens.findErr = errors.New("disk")

	_, err := fixture.authenticator.Authenticate(t.Context(), validToken())

	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrUnauthorized)
	assert.NotContains(t, fixture.logged.String(), tokenSecret)
	assert.NotContains(t, err.Error(), tokenSecret)
}
