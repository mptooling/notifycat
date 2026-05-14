// Package githubhook authenticates and parses inbound GitHub webhook
// requests. It exposes a constant-time HMAC verifier, an HTTP middleware that
// gates a downstream handler, and a JSON parser for the pull_request /
// pull_request_review payloads we care about.
package githubhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// SignatureHeader is the HTTP header GitHub uses to carry the HMAC-SHA256
// digest of the raw request body.
const SignatureHeader = "X-Hub-Signature-256"

// signaturePrefix is the only scheme we accept (GitHub's modern signature).
const signaturePrefix = "sha256="

// ErrInvalidSignature is returned when the signature does not match.
var ErrInvalidSignature = errors.New("githubhook: invalid signature")

// Verifier checks HMAC-SHA256 signatures against a shared secret.
type Verifier struct {
	secret []byte
}

// NewVerifier returns a Verifier configured with the given shared secret.
func NewVerifier(secret string) *Verifier {
	return &Verifier{secret: []byte(secret)}
}

// Verify checks that signature is a valid "sha256=<hex>" HMAC of body using
// the verifier's secret. Returns ErrInvalidSignature for any mismatch.
//
// The comparison runs in constant time to prevent timing oracles.
func (v *Verifier) Verify(body []byte, signature string) error {
	if !strings.HasPrefix(signature, signaturePrefix) {
		return ErrInvalidSignature
	}
	provided, err := hex.DecodeString(signature[len(signaturePrefix):])
	if err != nil {
		return ErrInvalidSignature
	}

	mac := hmac.New(sha256.New, v.secret)
	mac.Write(body)
	expected := mac.Sum(nil)

	if !hmac.Equal(provided, expected) {
		return ErrInvalidSignature
	}
	return nil
}
