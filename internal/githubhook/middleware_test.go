package githubhook_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/githubhook"
)

func TestSignatureMiddleware_PassesValid(t *testing.T) {
	v := githubhook.NewVerifier(testSecret)
	called := false
	var seenBody []byte

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		seenBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	body := []byte(`{"foo":"bar"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set(githubhook.SignatureHeader, sign(body))
	rec := httptest.NewRecorder()

	githubhook.SignatureMiddleware(v)(next).ServeHTTP(rec, req)

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

func TestSignatureMiddleware_Rejects401OnInvalid(t *testing.T) {
	v := githubhook.NewVerifier(testSecret)
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called for invalid signature")
	})

	body := []byte(`{"foo":"bar"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set(githubhook.SignatureHeader, "sha256=deadbeef")
	rec := httptest.NewRecorder()

	githubhook.SignatureMiddleware(v)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
}

func TestSignatureMiddleware_RejectsMissingSignature(t *testing.T) {
	v := githubhook.NewVerifier(testSecret)
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called when signature is missing")
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	githubhook.SignatureMiddleware(v)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
}

func TestSignatureMiddleware_BodyTooLargeReturns413(t *testing.T) {
	v := githubhook.NewVerifier(testSecret)
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called when body is too large")
	})

	// Build a body that exceeds the limit.
	big := bytes.Repeat([]byte("a"), int(githubhook.MaxBodyBytes)+1)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(big))
	req.Header.Set(githubhook.SignatureHeader, sign(big))
	rec := httptest.NewRecorder()
	githubhook.SignatureMiddleware(v)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d; want 413", rec.Code)
	}
}
