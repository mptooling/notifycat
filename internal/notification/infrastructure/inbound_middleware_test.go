package infrastructure_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/notification/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/security"
)

const middlewareTestSecret = "topsecret"

// rejectingHandler fails the test if the middleware ever lets a request through.
func rejectingHandler(t *testing.T) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		assert.Fail(t, "next handler must not be called")
	})
}

func TestSignatureMiddleware_PassesValid(t *testing.T) {
	var seenBody []byte
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		seenBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	body := []byte(`{"foo":"bar"}`)
	request := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	request.Header.Set(security.SignatureHeader, security.Sign(middlewareTestSecret, body))
	recorder := httptest.NewRecorder()

	infrastructure.SignatureMiddleware(security.NewGitHubVerifier(middlewareTestSecret))(next).ServeHTTP(recorder, request)

	require.True(t, called, "a valid signature must reach the handler")
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, body, seenBody, "the body is replayed intact downstream")
}

func TestSignatureMiddleware_Rejects401OnInvalid(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(`{"foo":"bar"}`))
	request.Header.Set(security.SignatureHeader, "sha256=deadbeef")
	recorder := httptest.NewRecorder()

	infrastructure.SignatureMiddleware(security.NewGitHubVerifier(middlewareTestSecret))(rejectingHandler(t)).ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestSignatureMiddleware_RejectsMissingSignature(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader("{}"))
	recorder := httptest.NewRecorder()

	infrastructure.SignatureMiddleware(security.NewGitHubVerifier(middlewareTestSecret))(rejectingHandler(t)).ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestSignatureMiddleware_BodyTooLargeReturns413(t *testing.T) {
	oversized := bytes.Repeat([]byte("a"), int(infrastructure.MaxBodyBytes)+1)
	request := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(oversized))
	request.Header.Set(security.SignatureHeader, security.Sign(middlewareTestSecret, oversized))
	recorder := httptest.NewRecorder()

	infrastructure.SignatureMiddleware(security.NewGitHubVerifier(middlewareTestSecret))(rejectingHandler(t)).ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}
