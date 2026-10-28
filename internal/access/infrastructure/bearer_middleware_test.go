package infrastructure_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/access/infrastructure"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestBearerMiddleware_PutsPrincipalInContext(t *testing.T) {
	authenticator := &fakeTokenAuthenticator{principal: domain.Principal{TokenID: "t1", User: "alice"}}
	var gotPrincipal domain.Principal
	next := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		gotPrincipal, _ = domain.PrincipalFrom(request.Context())
	})
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+validToken)

	infrastructure.BearerMiddleware(authenticator, discardLogger())(next).ServeHTTP(httptest.NewRecorder(), request)

	assert.Equal(t, domain.Principal{TokenID: "t1", User: "alice"}, gotPrincipal)
	assert.Equal(t, validToken, authenticator.gotRaw)
}

func TestBearerMiddleware_AcceptsHeaderVariants(t *testing.T) {
	testCases := []string{"bearer " + validToken, "Bearer  " + validToken + "\t", "BEARER " + validToken}
	for _, header := range testCases {
		t.Run(header, func(t *testing.T) {
			authenticator := &fakeTokenAuthenticator{}
			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			request.Header.Set("Authorization", header)

			infrastructure.BearerMiddleware(authenticator, discardLogger())(
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			).ServeHTTP(httptest.NewRecorder(), request)

			assert.Equal(t, validToken, authenticator.gotRaw)
		})
	}
}

func TestBearerMiddleware_EveryFailureIsTheSame401(t *testing.T) {
	testCases := []struct {
		name   string
		header string
		err    error
	}{
		{name: "no header"},
		{name: "other scheme", header: "Basic abc"},
		{name: "empty credential", header: "Bearer "},
		{name: "invite scheme", header: "Invite " + validToken},
		{name: "unknown", header: "Bearer " + validToken, err: &domain.AuthError{Reason: domain.AuthFailureUnknown}},
		{name: "mismatch", header: "Bearer " + validToken, err: &domain.AuthError{Reason: domain.AuthFailureMismatch}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			authenticator := &fakeTokenAuthenticator{err: testCase.err}
			if testCase.err == nil {
				authenticator.err = &domain.AuthError{Reason: domain.AuthFailureMalformed}
			}
			nextCalled := false
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true })
			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			if testCase.header != "" {
				request.Header.Set("Authorization", testCase.header)
			}
			recorder := httptest.NewRecorder()

			infrastructure.BearerMiddleware(authenticator, discardLogger())(next).ServeHTTP(recorder, request)

			assert.Equal(t, http.StatusUnauthorized, recorder.Code)
			assert.JSONEq(t, `{"error":"unauthorized"}`, recorder.Body.String())
			assert.Equal(t, "Bearer", recorder.Header().Get("WWW-Authenticate"))
			assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			assert.False(t, nextCalled)
		})
	}
}

func TestBearerMiddleware_AuthenticatorErrorIs500(t *testing.T) {
	authenticator := &fakeTokenAuthenticator{err: errors.New("disk")}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+validToken)
	recorder := httptest.NewRecorder()

	infrastructure.BearerMiddleware(authenticator, discardLogger())(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.JSONEq(t, `{"error":"internal_error"}`, recorder.Body.String())
}
