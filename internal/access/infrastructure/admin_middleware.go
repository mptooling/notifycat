package infrastructure

import (
	"net/http"

	"github.com/mptooling/notifycat/internal/access/domain"
)

// AdminMiddleware rejects requests that do not carry the admin key as a Bearer credential.
func AdminMiddleware(authenticator domain.AdminAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if err := authenticator.Authenticate(request.Context(), credentialFromHeader(request, domain.BearerScheme)); err != nil {
				writeUnauthorized(w, domain.BearerScheme)
				return
			}
			next.ServeHTTP(w, request)
		})
	}
}
