package slackhook

import (
	"bytes"
	"errors"
	"io"
	"net/http"
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
			timestamp := r.Header.Get(TimestampHeader)
			if signature == "" || timestamp == "" {
				http.Error(w, "missing signature", http.StatusUnauthorized)
				return
			}
			if err := verifier.Verify(timestamp, body, signature); err != nil {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}

			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			next.ServeHTTP(w, r)
		})
	}
}
