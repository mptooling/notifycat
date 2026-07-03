// Package maintenance wires the maintenance domain — stale-message cleanup and
// PR reconcile — into an fx module. This file is the only fx-aware part of the
// domain; the domain, application, and infrastructure layers stay
// framework-free.
package maintenance

import (
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/maintenance/application"
	"github.com/mptooling/notifycat/internal/maintenance/domain"
	"github.com/mptooling/notifycat/internal/maintenance/infrastructure"
)

// Config carries the maintenance module's runtime configuration — the
// stale-message TTL and the reconcile dry-run flag — supplied as a single value
// by the composition root (or a test), avoiding fx collisions between bare
// scalars.
type Config struct {
	TTL    time.Duration
	DryRun bool
}

// Module binds the maintenance ports to their adapters and use cases. It
// expects the composition root to supply the external inputs it cannot build
// itself: the store's *store.PullRequests, an infrastructure.PRStateGetter
// (the GitHub client), a *slog.Logger, and a Config.
var Module = fx.Module("maintenance",
	fx.Provide(
		fx.Annotate(
			infrastructure.NewPRRepository,
			fx.As(new(domain.StaleMessageDeleter)),
			fx.As(new(domain.OpenLister)),
			fx.As(new(domain.Closer)),
			fx.As(new(domain.Deleter)),
		),
		fx.Annotate(
			infrastructure.NewGitHubChecker,
			fx.As(new(domain.PRChecker)),
		),
		provideCleanerParams,
		fx.Annotate(
			application.NewCleaner,
			fx.As(new(domain.StaleMessageCleaner)),
		),
		provideReconcilerParams,
		fx.Annotate(
			application.NewReconciler,
			fx.As(new(domain.Reconciler)),
		),
	),
)

// provideCleanerParams assembles the cleaner's domain params from the graph.
// Interval is fixed at the domain constant; the clock is time.Now.
func provideCleanerParams(deleter domain.StaleMessageDeleter, logger *slog.Logger, cfg Config) domain.CleanerParams {
	return domain.CleanerParams{
		Deleter:  deleter,
		TTL:      cfg.TTL,
		Interval: domain.Interval,
		Logger:   logger,
		Now:      time.Now,
	}
}

// provideReconcilerParams assembles the reconciler's domain params from the graph.
func provideReconcilerParams(lister domain.OpenLister, checker domain.PRChecker, closer domain.Closer, deleter domain.Deleter, logger *slog.Logger, cfg Config) domain.ReconcilerParams {
	return domain.ReconcilerParams{
		Lister:  lister,
		Checker: checker,
		Closer:  closer,
		Deleter: deleter,
		Logger:  logger,
		DryRun:  cfg.DryRun,
	}
}
