package access_test

import (
	"io"
	"log/slog"
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/mptooling/notifycat/internal/access"
	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func TestModule_GraphResolves(t *testing.T) {
	db := persistence.NewTestDB(t)

	app := fxtest.New(t,
		access.Module,
		fx.Provide(
			func() *persistence.AccessInvites { return persistence.NewAccessInvites(db) },
			func() *persistence.AccessTokens { return persistence.NewAccessTokens(db) },
			func() domain.Settings { return domain.Settings{} },
			func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) },
		),
		fx.Invoke(func(
			domain.InviteIssuer,
			domain.InviteRedeemer,
			domain.TokenAdmin,
			domain.TokenAuthenticator,
			domain.AdminAuthenticator,
		) {
		}),
	)

	app.RequireStart()
	app.RequireStop()
}
