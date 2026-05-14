package githubhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/mptooling/notifycat/internal/githubhook"
)

const testSecret = "topsecret"

func sign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifier_ValidSignature(t *testing.T) {
	v := githubhook.NewVerifier(testSecret)
	body := []byte(`{"ok":true}`)

	if err := v.Verify(body, sign(body)); err != nil {
		t.Fatalf("Verify with valid signature returned %v; want nil", err)
	}
}

func TestVerifier_InvalidSignature(t *testing.T) {
	v := githubhook.NewVerifier(testSecret)
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

func TestVerifier_BodyTamperedReturnsError(t *testing.T) {
	v := githubhook.NewVerifier(testSecret)
	body := []byte(`{"ok":true}`)
	tampered := []byte(`{"ok":false}`)

	if err := v.Verify(tampered, sign(body)); err == nil {
		t.Fatal("Verify with tampered body returned nil; want error")
	}
}
