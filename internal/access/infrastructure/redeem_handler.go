package infrastructure

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/platform/httpx"
)

type redeemResponse struct {
	User  string `json:"user"`
	Token string `json:"token"`
}

// NewRedeemHandler returns the endpoint that exchanges an Invite credential for a token.
func NewRedeemHandler(redeemer domain.InviteRedeemer, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		issued, err := redeemer.Redeem(request.Context(), credentialFromHeader(request, domain.InviteScheme))
		if errors.Is(err, domain.ErrUnauthorized) {
			writeUnauthorized(w, domain.InviteScheme)
			return
		}
		if err != nil {
			logger.ErrorContext(request.Context(), "agent api request failed", "err", err)
			httpx.WriteError(w, http.StatusInternalServerError, string(domain.ErrorCodeInternal))
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, redeemResponse{User: issued.Token.User, Token: issued.Secret})
	})
}
