package security_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/security"
)

// Bitbucket signs a delivery with the same raw-body scheme as GitHub, so the
// canonical HMAC-SHA256 vector (secret "It's a Secret to Everybody", body
// "Hello World!") applies unchanged.
func TestBitbucketVerifier_PublishedVector(t *testing.T) {
	const secret = "It's a Secret to Everybody"
	body := []byte("Hello World!")
	verifier := security.NewBitbucketVerifier(secret)

	require.NoError(t, verifier.Verify(body, sign(secret, body)))
	assert.Error(t, verifier.Verify([]byte("Goodbye World!"), sign(secret, body)), "a tampered body must not verify")
}

func TestBitbucketVerifier_RejectsBadSignatures(t *testing.T) {
	verifier := security.NewBitbucketVerifier(testSecret)
	body := []byte(`{"ok":true}`)

	for name, signature := range badSignatures {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, verifier.Verify(body, signature))
		})
	}
}
