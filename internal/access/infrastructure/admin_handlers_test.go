package infrastructure_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/access/infrastructure"
)

type adminFixture struct {
	authenticator *fakeAdminAuthenticator
	issuer        *fakeInviteIssuer
	tokens        *fakeTokenAdmin
	handler       http.Handler
}

func newAdminFixture() adminFixture {
	fixture := adminFixture{
		authenticator: &fakeAdminAuthenticator{},
		issuer:        &fakeInviteIssuer{},
		tokens:        &fakeTokenAdmin{},
	}
	fixture.handler = infrastructure.NewAdminHandler(domain.AdminHandlerParams{
		Authenticator: fixture.authenticator,
		Invites:       fixture.issuer,
		Tokens:        fixture.tokens,
		Logger:        discardLogger(),
	})
	return fixture
}

func (fixture adminFixture) do(method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer nca_key")
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, request)
	return recorder
}

func TestAdminHandler_CreateInvite(t *testing.T) {
	fixture := newAdminFixture()
	fixture.issuer.issued = domain.IssuedInvite{
		Invite:        domain.Invite{User: "alice", ExpiresAt: time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)},
		Secret:        "nci_x",
		RedeemCommand: "curl …",
	}

	recorder := fixture.do(http.MethodPost, "/admin/v1/invites", `{"user":"alice"}`)

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.JSONEq(t, `{"invite":"nci_x","user":"alice","expires_at":"2026-10-28T10:00:00Z","redeem_command":"curl …"}`, recorder.Body.String())
	assert.Equal(t, "alice", fixture.issuer.gotUser)
}

func TestAdminHandler_CreateInvite_BadRequests(t *testing.T) {
	testCases := []struct {
		name       string
		body       string
		issuerErr  error
		wantCode   string
		wantStatus int
	}{
		{name: "bad json", body: "not json", wantCode: "invalid_body", wantStatus: http.StatusBadRequest},
		{name: "too large", body: strings.Repeat("x", 16<<10+1), wantCode: "invalid_body", wantStatus: http.StatusBadRequest},
		{name: "invalid user", body: `{"user":"Alice"}`, issuerErr: domain.ErrInvalidUser, wantCode: "invalid_user", wantStatus: http.StatusBadRequest},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newAdminFixture()
			fixture.issuer.err = testCase.issuerErr

			recorder := fixture.do(http.MethodPost, "/admin/v1/invites", testCase.body)

			assert.Equal(t, testCase.wantStatus, recorder.Code)
			assert.JSONEq(t, `{"error":"`+testCase.wantCode+`"}`, recorder.Body.String())
		})
	}
}

func TestAdminHandler_CreateInvite_StoreErrorIs500(t *testing.T) {
	fixture := newAdminFixture()
	fixture.issuer.err = assert.AnError

	recorder := fixture.do(http.MethodPost, "/admin/v1/invites", `{"user":"alice"}`)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.JSONEq(t, `{"error":"internal_error"}`, recorder.Body.String())
}

func TestAdminHandler_ListTokens(t *testing.T) {
	fixture := newAdminFixture()
	usedAt := time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)
	fixture.tokens.tokens = []domain.Token{
		{ID: "t1", User: "alice", CreatedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)},
		{ID: "t2", User: "bob", CreatedAt: time.Date(2026, 10, 28, 9, 5, 0, 0, time.UTC), LastUsedAt: &usedAt},
	}

	recorder := fixture.do(http.MethodGet, "/admin/v1/tokens", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"tokens":[{"id":"t1","user":"alice","created_at":"2026-10-28T09:00:00Z","last_used_at":null},
		{"id":"t2","user":"bob","created_at":"2026-10-28T09:05:00Z","last_used_at":"2026-10-28T09:30:00Z"}]}`, recorder.Body.String())
}

func TestAdminHandler_ListTokens_EmptyIsEmptyArray(t *testing.T) {
	fixture := newAdminFixture()

	recorder := fixture.do(http.MethodGet, "/admin/v1/tokens", "")

	assert.JSONEq(t, `{"tokens":[]}`, recorder.Body.String())
}

func TestAdminHandler_DeleteToken(t *testing.T) {
	fixture := newAdminFixture()

	recorder := fixture.do(http.MethodDelete, "/admin/v1/tokens/t1", "")

	assert.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, []string{"t1"}, fixture.tokens.deleted)
}

func TestAdminHandler_DeleteToken_UnknownIs404(t *testing.T) {
	fixture := newAdminFixture()
	fixture.tokens.err = domain.ErrTokenNotFound

	recorder := fixture.do(http.MethodDelete, "/admin/v1/tokens/nope", "")

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.JSONEq(t, `{"error":"not_found"}`, recorder.Body.String())
}

func TestAdminHandler_RequiresAdminKey(t *testing.T) {
	testCases := []struct{ method, path string }{
		{http.MethodPost, "/admin/v1/invites"},
		{http.MethodGet, "/admin/v1/tokens"},
		{http.MethodDelete, "/admin/v1/tokens/t1"},
		{http.MethodGet, "/admin/v1/unknown"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.method+" "+testCase.path, func(t *testing.T) {
			fixture := newAdminFixture()
			fixture.authenticator.err = domain.ErrUnauthorized

			recorder := fixture.do(testCase.method, testCase.path, `{"user":"alice"}`)

			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			assert.JSONEq(t, `{"error":"unauthorized"}`, recorder.Body.String())
			assert.Equal(t, "Bearer", recorder.Header().Get("WWW-Authenticate"))
			assert.Empty(t, fixture.issuer.gotUser, "no admin use case runs without the key")
			assert.Empty(t, fixture.tokens.deleted, "no admin use case runs without the key")
		})
	}
}
