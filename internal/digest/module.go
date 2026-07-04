// Package digest wires the digest domain — the scheduled stuck-PR reminder —
// into an fx module. This file is the only fx-aware part of the domain; the
// domain, application, and infrastructure layers stay framework-free.
package digest

import (
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/digest/application"
	"github.com/mptooling/notifycat/internal/digest/domain"
	"github.com/mptooling/notifycat/internal/digest/infrastructure"
)

// Config carries the digest module's runtime configuration — the distinct
// enabled cron specs and the digest timezone — supplied as a single value by
// the composition root (or a test).
type Config struct {
	Specs []string
	TZ    *time.Location
}

// Module binds the digest ports to their adapters and use cases. It expects the
// composition root to supply the external inputs it cannot build itself: the
// store's *store.PullRequests, a *slack.Composer, a *slack.Client, the routing
// provider (as domain.MappingLookup and domain.DigestResolver), a *slog.Logger,
// and a Config.
var Module = fx.Module("digest",
	fx.Provide(
		fx.Annotate(infrastructure.NewStuckRepo, fx.As(new(domain.StuckFinder))),
		fx.Annotate(infrastructure.NewSlackComposer, fx.As(new(domain.DigestComposer))),
		fx.Annotate(infrastructure.NewSlackPoster, fx.As(new(domain.DigestPoster))),
		provideReporterParams,
		fx.Annotate(application.NewReporter, fx.As(new(domain.DigestReporter)), fx.As(new(domain.ScheduleJob))),
		provideSchedulerParams,
		fx.Annotate(application.NewScheduler, fx.As(new(domain.DigestScheduler))),
	),
)

// provideReporterParams assembles the reporter's domain params from the graph.
// The clock is time.Now; the timezone comes from Config.
func provideReporterParams(finder domain.StuckFinder, mappings domain.MappingLookup, poster domain.DigestPoster, composer domain.DigestComposer, digests domain.DigestResolver, logger *slog.Logger, cfg Config) domain.ReporterParams {
	return domain.ReporterParams{
		Finder:   finder,
		Mappings: mappings,
		Poster:   poster,
		Composer: composer,
		Digests:  digests,
		Logger:   logger,
		TZ:       cfg.TZ,
		Now:      time.Now,
	}
}

// provideSchedulerParams assembles the scheduler's domain params from the graph.
func provideSchedulerParams(job domain.ScheduleJob, logger *slog.Logger, cfg Config) domain.SchedulerParams {
	return domain.SchedulerParams{
		Specs:  cfg.Specs,
		Job:    job,
		Logger: logger,
		TZ:     cfg.TZ,
	}
}
