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

	"github.com/mptooling/notifycat/internal/platform/security"
)

const testSecret = "8f742231b10e8888abcd99yyyzzz85a5"

// fixedClock is the reference "now" the tests verify against. Slack signs the
// request timestamp into the base string, so the verifier's clock and the
// timestamp used to sign must agree (within the replay window).
var fixedClock = time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

func clockAt(t time.Time) security.SlackOption {
	return security.WithSlackClock(func() time.Time { return t })
}

// sign builds the "v0=<hex>" Slack signature of body for the given timestamp.
func sign(timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write(body)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func tsString(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10)
}

func signedRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	ts := tsString(fixedClock)
	req := httptest.NewRequest(http.MethodPost, "/webhook/slack/interactions", bytes.NewReader(body))
	req.Header.Set(security.SlackSignatureHeader, sign(ts, body))
	req.Header.Set(security.SlackTimestampHeader, ts)
	return req
}

func TestSignatureMiddleware_PassesValid(t *testing.T) {
	v := security.NewSlackVerifier(testSecret, clockAt(fixedClock))
	called := false
	var seenBody []byte
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		seenBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	body := []byte(`payload=%7B%22type%22%3A%22block_actions%22%7D`)
	rec := httptest.NewRecorder()
	SignatureMiddleware(v)(next).ServeHTTP(rec, signedRequest(t, body))

	if !called {
		t.Fatal("next handler not invoked on valid signature")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
	if !bytes.Equal(seenBody, body) {
		t.Errorf("downstream body = %q; want %q", seenBody, body)
	}
}

func TestSignatureMiddleware_RejectsForged(t *testing.T) {
	v := security.NewSlackVerifier(testSecret, clockAt(fixedClock))
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called for a forged signature")
	})

	body := []byte("payload=x")
	req := signedRequest(t, body)
	req.Header.Set(security.SlackSignatureHeader, "v0=deadbeef")
	rec := httptest.NewRecorder()
	SignatureMiddleware(v)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
}

func TestSignatureMiddleware_RejectsMissingSignature(t *testing.T) {
	v := security.NewSlackVerifier(testSecret, clockAt(fixedClock))
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called when signature is missing")
	})

	req := signedRequest(t, []byte("payload=x"))
	req.Header.Del(security.SlackSignatureHeader)
	rec := httptest.NewRecorder()
	SignatureMiddleware(v)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
}

func TestSignatureMiddleware_RejectsMissingTimestamp(t *testing.T) {
	v := security.NewSlackVerifier(testSecret, clockAt(fixedClock))
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called when timestamp is missing")
	})

	req := signedRequest(t, []byte("payload=x"))
	req.Header.Del(security.SlackTimestampHeader)
	rec := httptest.NewRecorder()
	SignatureMiddleware(v)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
}

func TestSignatureMiddleware_RejectsStaleTimestamp(t *testing.T) {
	// Verifier's clock is six minutes ahead of the signed timestamp.
	v := security.NewSlackVerifier(testSecret, clockAt(fixedClock.Add(6*time.Minute)))
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called for a stale timestamp")
	})

	rec := httptest.NewRecorder()
	SignatureMiddleware(v)(next).ServeHTTP(rec, signedRequest(t, []byte("payload=x")))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
}

func TestSignatureMiddleware_BodyTooLargeReturns413(t *testing.T) {
	v := security.NewSlackVerifier(testSecret, clockAt(fixedClock))
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called when body is too large")
	})

	big := bytes.Repeat([]byte("a"), int(MaxBodyBytes)+1)
	rec := httptest.NewRecorder()
	SignatureMiddleware(v)(next).ServeHTTP(rec, signedRequest(t, big))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d; want 413", rec.Code)
	}
}
