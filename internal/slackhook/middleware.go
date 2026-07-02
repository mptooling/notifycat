package slackhook

import (
	"net/http"

	"github.com/mptooling/notifycat/internal/webhook"
)

// MaxBodyBytes caps the size of an accepted interaction body. Slack's
// interaction payloads are small (a JSON envelope in a single form field), so
// 1 MiB is generous and guards against memory-exhaustion attacks.
const MaxBodyBytes int64 = 1 << 20 // 1 MiB

// SignatureMiddleware returns an HTTP middleware that:
//   - rejects any request whose body exceeds MaxBodyBytes (413),
//   - rejects any request missing the signature or timestamp header (401),
//   - rejects any request whose signature is invalid or whose timestamp is
//     stale (401),
//   - passes a fresh body reader to next, so downstream handlers can read the
//     verified body without juggling the raw stream themselves.
//
// The signature is verified over the raw bytes before any form parsing, exactly
// like internal/githubhook.
func SignatureMiddleware(verifier *Verifier) func(http.Handler) http.Handler {
	return webhook.Signature(MaxBodyBytes, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		signature := r.Header.Get(SignatureHeader)
		timestamp := r.Header.Get(TimestampHeader)
		if signature == "" || timestamp == "" {
			http.Error(w, "missing signature", http.StatusUnauthorized)
			return false
		}
		if err := verifier.Verify(timestamp, body, signature); err != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return false
		}
		return true
	})
}
