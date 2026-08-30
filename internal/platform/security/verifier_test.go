package security_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/security"
)

const testSecret = "topsecret"

// badSignatures are the malformed forms every raw-body verifier must reject.
var badSignatures = map[string]string{
	"wrong hex":       "sha256=" + hex.EncodeToString(make([]byte, 32)),
	"missing scheme":  hex.EncodeToString(make([]byte, 32)),
	"empty":           "",
	"wrong algorithm": "sha1=abcdef",
	"truncated":       "sha256=abc",
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifier_ValidSignature(t *testing.T) {
	verifier := security.NewGitHubVerifier(testSecret)
	body := []byte(`{"ok":true}`)

	err := verifier.Verify(body, sign(testSecret, body))

	assert.NoError(t, err)
}

func TestVerifier_InvalidSignature(t *testing.T) {
	verifier := security.NewGitHubVerifier(testSecret)
	body := []byte(`{"ok":true}`)

	for name, signature := range badSignatures {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, verifier.Verify(body, signature))
		})
	}
}

func TestSign_RoundTripsWithVerify(t *testing.T) {
	body := []byte(`{"ok":true}`)

	signature := security.Sign(testSecret, body)

	require.Equal(t, sign(testSecret, body), signature)
	assert.NoError(t, security.NewGitHubVerifier(testSecret).Verify(body, signature))
}
