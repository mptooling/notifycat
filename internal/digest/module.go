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
	"github.com/mptooling/notifycat/internal/kernel"
)

// Config carries the digest module's runtime configuration — the distinct
// enabled cron specs, the digest timezone, and the deployment's git provider —
// supplied as a single value by the composition root (or a test).
type Config struct {
	Specs    []string
	TZ       *time.Location
	Provider kernel.Provider
	// Country is the ISO 3166-1 alpha-2 code selecting the holiday table.
	// Empty means weekends only.
	Country string
}

// Module binds the digest ports to their adapters and use cases. It expects the
// composition root to supply the external inputs it cannot build itself: the
// store's *store.PullRequests, a *slack.Composer, a *slack.Client, the routing
// provider (as domain.DigestTargets and domain.DigestResolver), a *slog.Logger,
// and a Config.
var Module = fx.Module("digest",
	fx.Provide(
		fx.Annotate(infrastructure.NewStuckRepo, fx.As(new(domain.StuckFinder))),
		fx.Annotate(infrastructure.NewSlackComposer, fx.As(new(domain.DigestComposer))),
		fx.Annotate(infrastructure.NewSlackPoster, fx.As(new(domain.DigestPoster))),
		provideReporterParams,
		fx.Annotate(application.NewReporter, fx.As(new(domain.DigestReporter)), fx.As(new(domain.ScheduleJob))),
		fx.Annotate(provideCalendar, fx.As(new(domain.DigestCalendar))),
		provideSchedulerParams,
		fx.Annotate(application.NewScheduler, fx.As(new(domain.DigestScheduler))),
	),
)

// provideReporterParams assembles the reporter's domain params from the graph.
// The clock is time.Now; the timezone comes from Config.
func provideReporterParams(finder domain.StuckFinder, targets domain.DigestTargets, poster domain.DigestPoster, composer domain.DigestComposer, digests domain.DigestResolver, logger *slog.Logger, cfg Config) domain.ReporterParams {
	return domain.ReporterParams{
		Finder:   finder,
		Targets:  targets,
		Poster:   poster,
		Composer: composer,
		Digests:  digests,
		Logger:   logger,
		TZ:       cfg.TZ,
		Now:      time.Now,
		Provider: cfg.Provider,
	}
}

// provideCalendar builds the weekend/holiday calendar. An unset or unrecognized
// country degrades to weekends-only with a warning rather than failing the
// graph — see application.NewCalendar.
func provideCalendar(logger *slog.Logger, cfg Config) *application.Calendar {
	return application.NewCalendar(domain.CalendarParams{Country: cfg.Country, Logger: logger})
}

// provideSchedulerParams assembles the scheduler's domain params from the graph.
func provideSchedulerParams(job domain.ScheduleJob, calendar domain.DigestCalendar, logger *slog.Logger, cfg Config) domain.SchedulerParams {
	return domain.SchedulerParams{
		Specs:    cfg.Specs,
		Job:      job,
		Logger:   logger,
		TZ:       cfg.TZ,
		Calendar: calendar,
		Now:      time.Now,
	}
}
