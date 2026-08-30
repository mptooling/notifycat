package infrastructure_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/security"
)

func TestGitHubSigner_ProducesCorrectHeaderAndValue(t *testing.T) {
	signer := infrastructure.NewGitHubSigner()
	body := []byte(`{"action":"opened"}`)

	header, value := signer.Sign("topsecret", body)

	assert.Equal(t, security.SignatureHeader, header)
	assert.NoError(t, security.NewGitHubVerifier("topsecret").Verify(body, value),
		"the signature must verify through the production verifier")
}

func TestGitHubSigner_DifferentSecrets_ProduceDifferentValues(t *testing.T) {
	signer := infrastructure.NewGitHubSigner()
	body := []byte(`{"action":"opened"}`)

	_, first := signer.Sign("secret1", body)
	_, second := signer.Sign("secret2", body)

	assert.NotEqual(t, first, second)
}

func TestBitbucketSigner_ProducesCorrectHeaderAndValue(t *testing.T) {
	signer := infrastructure.NewBitbucketSigner()
	body := []byte(`{"action":"opened"}`)

	header, value := signer.Sign("topsecret", body)

	assert.Equal(t, security.SignatureHeaderBitbucket, header)
	assert.NoError(t, security.NewBitbucketVerifier("topsecret").Verify(body, value))
}

func TestBitbucketSigner_DifferentSecrets_ProduceDifferentValues(t *testing.T) {
	signer := infrastructure.NewBitbucketSigner()
	body := []byte(`{"action":"opened"}`)

	_, first := signer.Sign("secret1", body)
	_, second := signer.Sign("secret2", body)

	assert.NotEqual(t, first, second)
}
