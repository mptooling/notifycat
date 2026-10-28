package infrastructure

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/platform/httpx"
)

type createInviteRequest struct {
	User string `json:"user"`
}

type createInviteResponse struct {
	Invite        string `json:"invite"`
	User          string `json:"user"`
	ExpiresAt     string `json:"expires_at"`
	RedeemCommand string `json:"redeem_command"`
}

type tokenResponse struct {
	ID         string  `json:"id"`
	User       string  `json:"user"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
}

type listTokensResponse struct {
	Tokens []tokenResponse `json:"tokens"`
}

type adminHandler struct {
	invites domain.InviteIssuer
	tokens  domain.TokenAdmin
	logger  *slog.Logger
}

// NewAdminHandler returns the admin API, guarded by the admin key.
func NewAdminHandler(params domain.AdminHandlerParams) http.Handler {
	handler := &adminHandler{invites: params.Invites, tokens: params.Tokens, logger: params.Logger}
	mux := http.NewServeMux()
	mux.HandleFunc(domain.CreateInvitePattern, handler.createInvite)
	mux.HandleFunc(domain.ListTokensPattern, handler.listTokens)
	mux.HandleFunc(domain.DeleteTokenPattern, handler.deleteToken)
	return AdminMiddleware(params.Authenticator, params.Logger)(mux)
}

func (handler *adminHandler) createInvite(w http.ResponseWriter, request *http.Request) {
	var body createInviteRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, domain.MaxRequestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, string(domain.ErrorCodeInvalidBody))
		return
	}
	issued, err := handler.invites.Issue(request.Context(), body.User)
	if errors.Is(err, domain.ErrInvalidUser) {
		httpx.WriteError(w, http.StatusBadRequest, string(domain.ErrorCodeInvalidUser))
		return
	}
	if err != nil {
		handler.writeInternal(w, request, err)
		return
	}
	if httpx.PrefersText(request) {
		httpx.WriteText(w, http.StatusCreated, issued.RedeemCommand)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, createInviteResponse{
		Invite:        issued.Secret,
		User:          issued.Invite.User,
		ExpiresAt:     formatTime(issued.Invite.ExpiresAt),
		RedeemCommand: issued.RedeemCommand,
	})
}

func (handler *adminHandler) listTokens(w http.ResponseWriter, request *http.Request) {
	tokens, err := handler.tokens.List(request.Context())
	if err != nil {
		handler.writeInternal(w, request, err)
		return
	}
	response := listTokensResponse{Tokens: make([]tokenResponse, 0, len(tokens))}
	for _, token := range tokens {
		item := tokenResponse{ID: token.ID, User: token.User, CreatedAt: formatTime(token.CreatedAt)}
		if token.LastUsedAt != nil {
			lastUsedAt := formatTime(*token.LastUsedAt)
			item.LastUsedAt = &lastUsedAt
		}
		response.Tokens = append(response.Tokens, item)
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (handler *adminHandler) deleteToken(w http.ResponseWriter, request *http.Request) {
	err := handler.tokens.Delete(request.Context(), request.PathValue(domain.TokenIDPathValue))
	if errors.Is(err, domain.ErrTokenNotFound) {
		httpx.WriteError(w, http.StatusNotFound, string(domain.ErrorCodeNotFound))
		return
	}
	if err != nil {
		handler.writeInternal(w, request, err)
		return
	}
	httpx.WriteNoContent(w)
}

func (handler *adminHandler) writeInternal(w http.ResponseWriter, request *http.Request, err error) {
	handler.logger.ErrorContext(request.Context(), "admin request failed", "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, string(domain.ErrorCodeInternal))
}

func formatTime(moment time.Time) string {
	return moment.UTC().Format(time.RFC3339)
}
