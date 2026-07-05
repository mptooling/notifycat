package infrastructure_test

import (
	"testing"

	"github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/security"
)

func TestGitHubSigner_ProducesCorrectHeaderAndValue(t *testing.T) {
	signer := infrastructure.NewGitHubSigner()
	secret := "topsecret"
	body := []byte(`{"action":"opened"}`)

	header, value := signer.Sign(secret, body)

	if header != security.SignatureHeader {
		t.Errorf("header = %q; want %q", header, security.SignatureHeader)
	}
	// The value must verify correctly via the standard GitHubVerifier.
	if err := security.NewGitHubVerifier(secret).Verify(body, value); err != nil {
		t.Errorf("Verify of Sign output returned %v; want nil", err)
	}
}

func TestGitHubSigner_DifferentSecrets_ProduceDifferentValues(t *testing.T) {
	signer := infrastructure.NewGitHubSigner()
	body := []byte(`{"action":"opened"}`)

	_, v1 := signer.Sign("secret1", body)
	_, v2 := signer.Sign("secret2", body)

	if v1 == v2 {
		t.Error("same signature for different secrets; want distinct values")
	}
}

func TestBitbucketSigner_ProducesCorrectHeaderAndValue(t *testing.T) {
	signer := infrastructure.NewBitbucketSigner()
	secret := "topsecret"
	body := []byte(`{"action":"opened"}`)

	header, value := signer.Sign(secret, body)

	if header != security.SignatureHeaderBitbucket {
		t.Errorf("header = %q; want %q", header, security.SignatureHeaderBitbucket)
	}
	if err := security.NewBitbucketVerifier(secret).Verify(body, value); err != nil {
		t.Errorf("Verify of Sign output returned %v; want nil", err)
	}
}

func TestBitbucketSigner_DifferentSecrets_ProduceDifferentValues(t *testing.T) {
	signer := infrastructure.NewBitbucketSigner()
	body := []byte(`{"action":"opened"}`)

	_, v1 := signer.Sign("secret1", body)
	_, v2 := signer.Sign("secret2", body)

	if v1 == v2 {
		t.Error("same signature for different secrets; want distinct values")
	}
}
