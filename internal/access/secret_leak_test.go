package access_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"golang.org/x/crypto/bcrypt"

	"github.com/mptooling/notifycat/internal/access"
	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/access/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

const (
	leakTestAdminKey      = "nca_AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_abcde"
	leakTestWrongAdminKey = "nca_ZyXwVuTsRqPoNmLkJiHgFeDcBa9876543210-_edcba"
	leakTestOtherSecret   = "QwErTyUiOpAsDfGhJkLzXcVbNm0123456789-_mnbvc"
	leakTestUnknownID     = "0123456789abcdef0123456789abcdef"
)

type accessFlow struct {
	credentials []string
}

func (flow *accessFlow) remember(t *testing.T, wire, prefix string) {
	t.Helper()

	credential, err := domain.ParseCredential(wire, prefix)
	require.NoError(t, err)
	flow.credentials = append(flow.credentials, wire, credential.Secret)
}

func serve(handler http.Handler, method, path, body, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func runAccessFlow(
	t *testing.T,
	adminHandler, redeemHandler, bearerHandler http.Handler,
) accessFlow {
	t.Helper()

	var flow accessFlow
	flow.credentials = append(flow.credentials, leakTestAdminKey, strings.TrimPrefix(leakTestAdminKey, domain.AdminKeyPrefix),
		leakTestWrongAdminKey, strings.TrimPrefix(leakTestWrongAdminKey, domain.AdminKeyPrefix), leakTestOtherSecret)

	created := serve(adminHandler, http.MethodPost, "/admin/v1/invites", `{"user":"alice"}`, "Bearer "+leakTestAdminKey)
	require.Equal(t, http.StatusCreated, created.Code)
	var invite struct {
		Invite string `json:"invite"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &invite))
	flow.remember(t, invite.Invite, domain.InvitePrefix)

	wrongAdmin := serve(adminHandler, http.MethodGet, "/admin/v1/tokens", "", "Bearer "+leakTestWrongAdminKey)
	require.Equal(t, http.StatusUnauthorized, wrongAdmin.Code)

	redeemed := serve(redeemHandler, http.MethodPost, "/api/v1/invites/redeem", "", "Invite "+invite.Invite)
	require.Equal(t, http.StatusCreated, redeemed.Code)
	var token struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(redeemed.Body.Bytes(), &token))
	flow.remember(t, token.Token, domain.TokenPrefix)

	redeemedAgain := serve(redeemHandler, http.MethodPost, "/api/v1/invites/redeem", "", "Invite "+invite.Invite)
	require.Equal(t, http.StatusUnauthorized, redeemedAgain.Code)

	parsedToken, err := domain.ParseCredential(token.Token, domain.TokenPrefix)
	require.NoError(t, err)
	unknownToken := domain.TokenPrefix + leakTestUnknownID + domain.CredentialSeparator + leakTestOtherSecret
	wrongSecretToken := domain.TokenPrefix + parsedToken.ID + domain.CredentialSeparator + leakTestOtherSecret

	bearerCalls := []struct {
		authorization string
		wantStatus    int
	}{
		{"Bearer " + token.Token, http.StatusOK},
		{"Bearer " + unknownToken, http.StatusUnauthorized},
		{"Bearer " + wrongSecretToken, http.StatusUnauthorized},
		{"Bearer not-a-credential", http.StatusUnauthorized},
	}
	for _, call := range bearerCalls {
		recorder := serve(bearerHandler, http.MethodGet, "/api/v1/probe", "", call.authorization)
		require.Equal(t, call.wantStatus, recorder.Code)
	}
	return flow
}

func TestAccess_NeverLogsCredentials(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	adminKeyHash, err := bcrypt.GenerateFromPassword([]byte(leakTestAdminKey), bcrypt.MinCost)
	require.NoError(t, err)
	db := persistence.NewTestDB(t)

	var (
		issuer        domain.InviteIssuer
		redeemer      domain.InviteRedeemer
		tokens        domain.TokenAdmin
		authenticator domain.TokenAuthenticator
		adminAuth     domain.AdminAuthenticator
	)
	app := fxtest.New(t,
		access.Module,
		fx.Provide(
			func() *persistence.AccessInvites { return persistence.NewAccessInvites(db) },
			func() *persistence.AccessTokens { return persistence.NewAccessTokens(db) },
			func() domain.Settings {
				return domain.Settings{AdminKeyHash: string(adminKeyHash), PublicDomain: "notifycat.example.com"}
			},
			func() *slog.Logger { return logger },
		),
		fx.Populate(&issuer, &redeemer, &tokens, &authenticator, &adminAuth),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)

	adminHandler := infrastructure.NewAdminHandler(domain.AdminHandlerParams{
		Authenticator: adminAuth, Invites: issuer, Tokens: tokens, Logger: logger,
	})
	redeemHandler := infrastructure.NewRedeemHandler(redeemer, logger)
	bearerHandler := infrastructure.BearerMiddleware(authenticator, logger)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	flow := runAccessFlow(t, adminHandler, redeemHandler, bearerHandler)

	output := logged.String()
	for _, message := range []string{"agent invite created", "agent token created", "agent invite redeem failed",
		"agent api auth failed", "admin auth failed"} {
		assert.Contains(t, output, message, "the flow really logged")
	}
	for _, secret := range append(flow.credentials, "Bearer ", "Invite nci_", "$2a$") {
		assert.NotContains(t, output, secret)
	}
}
