package slackhook

import (
	"net/http"

	"github.com/mptooling/notifycat/internal/platform/httpx"
	"github.com/mptooling/notifycat/internal/platform/security"
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
// like the GitHub webhook receiver in notification/infrastructure.
func SignatureMiddleware(verifier security.SignatureVerifier) func(http.Handler) http.Handler {
	return httpx.Signature(MaxBodyBytes, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		signature := r.Header.Get(security.SlackSignatureHeader)
		timestamp := r.Header.Get(security.SlackTimestampHeader)
		if signature == "" || timestamp == "" {
			http.Error(w, "missing signature", http.StatusUnauthorized)
			return false
		}
		// Build the compound "<timestamp>\n<v0=hex>" string expected by
		// security.SlackVerifier.Verify, which must satisfy the two-argument
		// SignatureVerifier interface while still covering the timestamp.
		compound := timestamp + "\n" + signature
		if err := verifier.Verify(body, compound); err != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return false
		}
		return true
	})
}
