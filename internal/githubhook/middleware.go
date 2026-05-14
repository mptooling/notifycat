package githubhook

import (
	"bytes"
	"errors"
	"io"
	"net/http"
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
func SignatureMiddleware(v *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limited := http.MaxBytesReader(w, r.Body, MaxBodyBytes)
			body, err := io.ReadAll(limited)
			if err != nil {
				var maxErr *http.MaxBytesError
				if errors.As(err, &maxErr) {
					http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
					return
				}
				http.Error(w, "read error", http.StatusBadRequest)
				return
			}

			signature := r.Header.Get(SignatureHeader)
			if signature == "" {
				http.Error(w, "missing signature", http.StatusUnauthorized)
				return
			}
			if err := v.Verify(body, signature); err != nil {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}

			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			next.ServeHTTP(w, r)
		})
	}
}
