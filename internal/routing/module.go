// Package routing wires the routing domain — repository→channel resolution
// across tiers and monorepo path rules — into an fx module. This file is the
// only fx-aware part of the domain; the domain, application, and infrastructure
// layers stay framework-free.
package routing

import (
	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/routing/application"
	"github.com/mptooling/notifycat/internal/routing/domain"
)

// Config carries the routing module's runtime configuration — the parsed
// mappings sections from config.yaml — supplied as a single value by the
// composition root (or a test).
type Config struct {
	Defaults domain.Defaults
	Mappings map[string]domain.Org
	Digest   *domain.DigestConfig
}

// Module binds the routing ports to their use cases. It expects the composition
// root to supply the external inputs it cannot build itself: a
// domain.ChangedFilesReader (the GitHub client), a *slog.Logger, and a Config.
var Module = fx.Module("routing",
	fx.Provide(
		fx.Annotate(provideProvider, fx.As(new(domain.RoutingProvider))),
		fx.Annotate(application.NewRouter, fx.As(new(domain.TargetResolver))),
	),
)

// provideProvider assembles the routing provider from the module Config.
func provideProvider(cfg Config) *application.Provider {
	return application.NewProvider(cfg.Defaults, cfg.Mappings, cfg.Digest)
}
