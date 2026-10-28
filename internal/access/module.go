// Package access wires the access domain — admin-issued invites and agent
// tokens — into an fx module. This file is the only fx-aware part of the domain.
package access

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/access/application"
	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/access/infrastructure"
)

// Module binds the access ports to their adapters and the five use cases. It
// expects the composition root to supply *persistence.AccessInvites,
// *persistence.AccessTokens, domain.Settings and a *slog.Logger.
var Module = fx.Module("access",
	fx.Provide(
		fx.Annotate(infrastructure.NewInviteRepo, fx.As(new(domain.InviteStore))),
		fx.Annotate(infrastructure.NewTokenRepo, fx.As(new(domain.TokenStore))),
		fx.Annotate(infrastructure.NewRandomSecret, fx.As(new(domain.SecretSource))),
		fx.Annotate(infrastructure.NewBcryptHasher, fx.As(new(domain.Hasher))),
		fx.Annotate(infrastructure.NewSystemClock, fx.As(new(domain.Clock))),
		fx.Annotate(provideInviteIssuer, fx.As(new(domain.InviteIssuer))),
		fx.Annotate(provideInviteRedeemer, fx.As(new(domain.InviteRedeemer))),
		fx.Annotate(application.NewTokenAdmin, fx.As(new(domain.TokenAdmin))),
		fx.Annotate(provideTokenAuthenticator, fx.As(new(domain.TokenAuthenticator))),
		fx.Annotate(application.NewAdminAuthenticator, fx.As(new(domain.AdminAuthenticator))),
	),
)

type inviteIssuerParams struct {
	fx.In
	Invites  domain.InviteStore
	Secrets  domain.SecretSource
	Hasher   domain.Hasher
	Clock    domain.Clock
	Settings domain.Settings
	Logger   *slog.Logger
}

func provideInviteIssuer(params inviteIssuerParams) *application.InviteIssuer {
	return application.NewInviteIssuer(domain.InviteIssuerParams{
		Invites:  params.Invites,
		Secrets:  params.Secrets,
		Hasher:   params.Hasher,
		Clock:    params.Clock,
		Settings: params.Settings,
		Logger:   params.Logger,
	})
}

type inviteRedeemerParams struct {
	fx.In
	Invites  domain.InviteStore
	Secrets  domain.SecretSource
	Hasher   domain.Hasher
	Clock    domain.Clock
	Settings domain.Settings
	Logger   *slog.Logger
}

func provideInviteRedeemer(params inviteRedeemerParams) *application.InviteRedeemer {
	return application.NewInviteRedeemer(domain.InviteRedeemerParams{
		Invites:  params.Invites,
		Secrets:  params.Secrets,
		Hasher:   params.Hasher,
		Clock:    params.Clock,
		Settings: params.Settings,
		Logger:   params.Logger,
	})
}

type tokenAuthenticatorParams struct {
	fx.In
	Tokens domain.TokenStore
	Hasher domain.Hasher
	Clock  domain.Clock
	Logger *slog.Logger
}

func provideTokenAuthenticator(params tokenAuthenticatorParams) *application.TokenAuthenticator {
	return application.NewTokenAuthenticator(domain.TokenAuthenticatorParams{
		Tokens: params.Tokens,
		Hasher: params.Hasher,
		Clock:  params.Clock,
		Logger: params.Logger,
	})
}
