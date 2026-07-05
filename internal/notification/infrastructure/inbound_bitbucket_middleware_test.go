package infrastructure_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/notification/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/security"
)

func signBitbucketBody(body []byte) string {
	mac := hmac.New(sha256.New, []byte(middlewareTestSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestBitbucketSignatureMiddleware_PassesValid(t *testing.T) {
	verifier := security.NewBitbucketVerifier(middlewareTestSecret)
	called := false
	var seenBody []byte

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		seenBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	body := []byte(`{"foo":"bar"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/bitbucket", bytes.NewReader(body))
	req.Header.Set(security.SignatureHeaderBitbucket, signBitbucketBody(body))
	rec := httptest.NewRecorder()

	infrastructure.BitbucketSignatureMiddleware(verifier)(next).ServeHTTP(rec, req)

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

func TestBitbucketSignatureMiddleware_Rejects401OnInvalid(t *testing.T) {
	verifier := security.NewBitbucketVerifier(middlewareTestSecret)
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called for invalid signature")
	})

	body := []byte(`{"foo":"bar"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/bitbucket", bytes.NewReader(body))
	req.Header.Set(security.SignatureHeaderBitbucket, "sha256=deadbeef")
	rec := httptest.NewRecorder()

	infrastructure.BitbucketSignatureMiddleware(verifier)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
}

func TestBitbucketSignatureMiddleware_RejectsMissingSignature(t *testing.T) {
	verifier := security.NewBitbucketVerifier(middlewareTestSecret)
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called when signature is missing")
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook/bitbucket", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	infrastructure.BitbucketSignatureMiddleware(verifier)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
}

func TestBitbucketSignatureMiddleware_BodyTooLargeReturns413(t *testing.T) {
	verifier := security.NewBitbucketVerifier(middlewareTestSecret)
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("next handler must not be called when body is too large")
	})

	big := bytes.Repeat([]byte("a"), int(infrastructure.MaxBodyBytes)+1)
	req := httptest.NewRequest(http.MethodPost, "/webhook/bitbucket", bytes.NewReader(big))
	req.Header.Set(security.SignatureHeaderBitbucket, signBitbucketBody(big))
	rec := httptest.NewRecorder()
	infrastructure.BitbucketSignatureMiddleware(verifier)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d; want 413", rec.Code)
	}
}
