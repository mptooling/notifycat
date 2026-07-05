// Package security holds notifycat's inbound-request authentication: signature
// verifiers for the signed webhooks it receives. It exposes a provider-agnostic
// SignatureVerifier port and the GitHub and Bitbucket raw-body HMAC-SHA256
// adapters (identical scheme; they differ only in the header the middleware
// reads).
package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// SignatureHeader is the HTTP header GitHub uses to carry the HMAC-SHA256 digest
// of the raw request body. SignatureHeaderBitbucket is Bitbucket's equivalent —
// same "sha256=<hex>" scheme, different header name.
const (
	SignatureHeader          = "X-Hub-Signature-256"
	SignatureHeaderBitbucket = "X-Hub-Signature"
)

// signaturePrefix is the only scheme accepted (GitHub's and Bitbucket's modern
// signature).
const signaturePrefix = "sha256="

// ErrInvalidSignature is returned when a signature does not match.
var ErrInvalidSignature = errors.New("security: invalid signature")

// SignatureVerifier verifies a signed request body against its signature header.
type SignatureVerifier interface {
	Verify(body []byte, signature string) error
}

// GitHubVerifier checks GitHub's HMAC-SHA256 signatures against a shared secret.
type GitHubVerifier struct {
	secret []byte
}

// NewGitHubVerifier returns a GitHubVerifier configured with the shared secret.
func NewGitHubVerifier(secret string) *GitHubVerifier {
	return &GitHubVerifier{secret: []byte(secret)}
}

// Sign returns the "sha256=<hex>" HMAC of body under secret — the value GitHub
// puts in X-Hub-Signature-256. It is the inverse of Verify and shares the same
// scheme, so anything Sign produces, Verify accepts. Used by the smoke command
// to forge a correctly-signed request against the live endpoint.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// Verify checks that signature is a valid "sha256=<hex>" HMAC of body using the
// verifier's secret. Returns ErrInvalidSignature for any mismatch. The
// comparison runs in constant time to prevent timing oracles.
func (v *GitHubVerifier) Verify(body []byte, signature string) error {
	return verifyRawBodyHMAC(v.secret, body, signature)
}

var _ SignatureVerifier = (*GitHubVerifier)(nil)

// verifyRawBodyHMAC is the shared "sha256=<hex>" raw-body HMAC-SHA256 check used
// by both the GitHub and Bitbucket verifiers. It rejects a wrong scheme,
// undecodable hex, or a digest mismatch (constant-time) with ErrInvalidSignature.
func verifyRawBodyHMAC(secret, body []byte, signature string) error {
	if !strings.HasPrefix(signature, signaturePrefix) {
		return ErrInvalidSignature
	}
	provided, err := hex.DecodeString(signature[len(signaturePrefix):])
	if err != nil {
		return ErrInvalidSignature
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	expected := mac.Sum(nil)

	if !hmac.Equal(provided, expected) {
		return ErrInvalidSignature
	}
	return nil
}
