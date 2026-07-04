package githubhook

import (
	"net/http"

	"github.com/mptooling/notifycat/internal/platform/httpx"
	"github.com/mptooling/notifycat/internal/platform/security"
)

// MaxBodyBytes caps the size of an accepted webhook body. GitHub limits its
// own webhook payloads to ~25 MiB but our handler should not need anywhere
// near that — a generous 1 MiB protects against memory exhaustion attacks.
const MaxBodyBytes int64 = 1 << 20 // 1 MiB

// SignatureMiddleware returns an HTTP middleware that:
//   - rejects any request whose body exceeds MaxBodyBytes (413),
//   - rejects any request whose X-Hub-Signature-256 header does not match
//     the HMAC of the body (401),
//   - passes a fresh body reader to next, so downstream handlers can read
//     the verified body without juggling the raw stream themselves.
func SignatureMiddleware(v security.SignatureVerifier) func(http.Handler) http.Handler {
	return httpx.Signature(MaxBodyBytes, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		signature := r.Header.Get(security.SignatureHeader)
		if signature == "" {
			http.Error(w, "missing signature", http.StatusUnauthorized)
			return false
		}
		if err := v.Verify(body, signature); err != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return false
		}
		return true
	})
}
