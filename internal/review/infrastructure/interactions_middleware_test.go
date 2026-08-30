package infrastructure

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/security"
)

const testSecret = "8f742231b10e8888abcd99yyyzzz85a5"

// fixedClock is the reference "now" the tests verify against. Slack signs the
// request timestamp into the base string, so the verifier's clock and the
// timestamp used to sign must agree (within the replay window).
var fixedClock = time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

func verifierAt(now time.Time) *security.SlackVerifier {
	return security.NewSlackVerifier(testSecret, security.WithSlackClock(func() time.Time { return now }))
}

// sign builds the "v0=<hex>" Slack signature of body for the given timestamp.
func sign(timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write(body)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func signedRequest(body []byte) *http.Request {
	timestamp := strconv.FormatInt(fixedClock.Unix(), 10)
	request := httptest.NewRequest(http.MethodPost, "/webhook/slack/interactions", bytes.NewReader(body))
	request.Header.Set(security.SlackSignatureHeader, sign(timestamp, body))
	request.Header.Set(security.SlackTimestampHeader, timestamp)
	return request
}

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
	body := []byte(`payload=%7B%22type%22%3A%22block_actions%22%7D`)
	recorder := httptest.NewRecorder()

	SignatureMiddleware(verifierAt(fixedClock))(next).ServeHTTP(recorder, signedRequest(body))

	require.True(t, called, "a valid signature must reach the handler")
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, body, seenBody, "the body is replayed intact downstream")
}

func TestSignatureMiddleware_RejectsForged(t *testing.T) {
	request := signedRequest([]byte("payload=x"))
	request.Header.Set(security.SlackSignatureHeader, "v0=deadbeef")
	recorder := httptest.NewRecorder()

	SignatureMiddleware(verifierAt(fixedClock))(rejectingHandler(t)).ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestSignatureMiddleware_RejectsMissingSignature(t *testing.T) {
	request := signedRequest([]byte("payload=x"))
	request.Header.Del(security.SlackSignatureHeader)
	recorder := httptest.NewRecorder()

	SignatureMiddleware(verifierAt(fixedClock))(rejectingHandler(t)).ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestSignatureMiddleware_RejectsMissingTimestamp(t *testing.T) {
	request := signedRequest([]byte("payload=x"))
	request.Header.Del(security.SlackTimestampHeader)
	recorder := httptest.NewRecorder()

	SignatureMiddleware(verifierAt(fixedClock))(rejectingHandler(t)).ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestSignatureMiddleware_RejectsStaleTimestamp(t *testing.T) {
	// The verifier's clock sits six minutes past the signed timestamp.
	recorder := httptest.NewRecorder()

	SignatureMiddleware(verifierAt(fixedClock.Add(6*time.Minute)))(rejectingHandler(t)).ServeHTTP(recorder, signedRequest([]byte("payload=x")))

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestSignatureMiddleware_BodyTooLargeReturns413(t *testing.T) {
	oversized := bytes.Repeat([]byte("a"), int(MaxBodyBytes)+1)
	recorder := httptest.NewRecorder()

	SignatureMiddleware(verifierAt(fixedClock))(rejectingHandler(t)).ServeHTTP(recorder, signedRequest(oversized))

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}
