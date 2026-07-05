package infrastructure

import (
	"github.com/mptooling/notifycat/internal/platform/security"
)

// GitHubSigner implements diagnosticsdomain.Signer using the GitHub HMAC-SHA256
// scheme from the platform security package.
type GitHubSigner struct{}

// NewGitHubSigner returns a GitHubSigner.
func NewGitHubSigner() *GitHubSigner { return &GitHubSigner{} }

// Sign returns (security.SignatureHeader, security.Sign(secret, body)).
func (GitHubSigner) Sign(secret string, body []byte) (header, value string) {
	return security.SignatureHeader, security.Sign(secret, body)
}

// BitbucketSigner implements diagnosticsdomain.Signer using the Bitbucket
// HMAC-SHA256 scheme from the platform security package.
type BitbucketSigner struct{}

// NewBitbucketSigner returns a BitbucketSigner.
func NewBitbucketSigner() *BitbucketSigner { return &BitbucketSigner{} }

// Sign returns (security.SignatureHeaderBitbucket, security.Sign(secret, body)).
func (BitbucketSigner) Sign(secret string, body []byte) (header, value string) {
	return security.SignatureHeaderBitbucket, security.Sign(secret, body)
}
