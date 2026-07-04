// Package httpx holds the HTTP plumbing shared by notifycat's signed-webhook
// receivers. It owns only the parts that are identical across providers —
// capping and reading the body once, and replaying a fresh body reader
// downstream — and delegates authentication to a provider-supplied callback. The
// signing scheme itself (which headers, what HMAC, which error message) stays
// with each provider, because those genuinely differ (GitHub signs the raw body;
// Slack signs a timestamped base string).
package httpx

import (
	"bytes"
	"errors"
	"io"
	"net/http"
)

// Authenticate verifies a request from its (already-read) raw body. On failure
// it writes the appropriate error response to w and returns false; on success it
// writes nothing and returns true. Keeping the failure response here — rather
// than returning an error the skeleton renders — lets each provider preserve its
// own status codes and messages (e.g. "missing signature" vs "invalid signature").
type Authenticate func(w http.ResponseWriter, r *http.Request, body []byte) bool

// Signature returns an HTTP middleware that rejects bodies larger than maxBytes
// (413), reads the body once, runs authenticate (which rejects unauthenticated
// requests itself), and otherwise replays a fresh body reader to next so
// downstream handlers read the verified bytes without touching the raw stream.
func Signature(maxBytes int64, authenticate Authenticate) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limited := http.MaxBytesReader(w, r.Body, maxBytes)
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

			if !authenticate(w, r, body) {
				return
			}

			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			next.ServeHTTP(w, r)
		})
	}
}
