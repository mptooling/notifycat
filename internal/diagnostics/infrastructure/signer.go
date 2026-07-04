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
