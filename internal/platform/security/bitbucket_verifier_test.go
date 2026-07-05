package security_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/mptooling/notifycat/internal/platform/security"
)

// bitbucketSign computes the "sha256=<hex>" raw-body HMAC the way Bitbucket signs
// a delivery — identical to GitHub's scheme.
func bitbucketSign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// TestBitbucketVerifier_PublishedVector runs the canonical HMAC-SHA256 test
// vector (secret "It's a Secret to Everybody", body "Hello World!") — the shared
// GitHub/Atlassian raw-body scheme — and confirms the verifier accepts its
// correctly-signed digest and rejects a tampered one.
func TestBitbucketVerifier_PublishedVector(t *testing.T) {
	const secret = "It's a Secret to Everybody"
	body := []byte("Hello World!")

	v := security.NewBitbucketVerifier(secret)
	if err := v.Verify(body, bitbucketSign(secret, body)); err != nil {
		t.Fatalf("Verify with valid signature returned %v; want nil", err)
	}
	if err := v.Verify([]byte("Goodbye World!"), bitbucketSign(secret, body)); err == nil {
		t.Fatal("Verify with tampered body returned nil; want error")
	}
}

func TestBitbucketVerifier_RejectsBadSignatures(t *testing.T) {
	v := security.NewBitbucketVerifier("topsecret")
	body := []byte(`{"ok":true}`)

	cases := map[string]string{
		"wrong hex":       "sha256=" + hex.EncodeToString(make([]byte, 32)),
		"missing scheme":  hex.EncodeToString(make([]byte, 32)),
		"empty":           "",
		"wrong algorithm": "sha1=abcdef",
		"truncated":       "sha256=abc",
	}
	for name, sig := range cases {
		t.Run(name, func(t *testing.T) {
			if err := v.Verify(body, sig); err == nil {
				t.Fatalf("Verify(%q) returned nil; want error", sig)
			}
		})
	}
}
