package infrastructure

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/platform/httpx"
)

// BearerMiddleware authenticates the Bearer token and passes the principal to next in the context.
func BearerMiddleware(authenticator domain.TokenAuthenticator, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			w.Header().Set(httpx.CacheControlHeader, httpx.NoStore)
			principal, err := authenticator.Authenticate(request.Context(), credentialFromHeader(request, domain.BearerScheme))
			if errors.Is(err, domain.ErrUnauthorized) {
				writeUnauthorized(w, domain.BearerScheme)
				return
			}
			if err != nil {
				logger.ErrorContext(request.Context(), "agent api request failed", "err", err)
				httpx.WriteError(w, http.StatusInternalServerError, string(domain.ErrorCodeInternal))
				return
			}
			next.ServeHTTP(w, request.WithContext(domain.WithPrincipal(request.Context(), principal)))
		})
	}
}

func writeUnauthorized(w http.ResponseWriter, scheme string) {
	w.Header().Set(domain.AuthenticateHeader, scheme)
	httpx.WriteError(w, http.StatusUnauthorized, string(domain.ErrorCodeUnauthorized))
}
