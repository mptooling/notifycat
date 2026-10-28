package infrastructure

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/platform/httpx"
)

// AdminMiddleware rejects requests that do not carry the admin key as a Bearer credential.
func AdminMiddleware(authenticator domain.AdminAuthenticator, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			err := authenticator.Authenticate(request.Context(), credentialFromHeader(request, domain.BearerScheme))
			if errors.Is(err, domain.ErrUnauthorized) {
				writeUnauthorized(w, domain.BearerScheme)
				return
			}
			if err != nil {
				logger.ErrorContext(request.Context(), "admin request failed", "err", err)
				httpx.WriteError(w, http.StatusInternalServerError, string(domain.ErrorCodeInternal))
				return
			}
			next.ServeHTTP(w, request)
		})
	}
}
