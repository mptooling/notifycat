package digest

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/robfig/cron/v3"
)

// Job is the unit the Scheduler fires on each cron tick. *Reporter satisfies it.
type Job interface {
	Report(ctx context.Context) error
}

// Scheduler runs a Job on a cron schedule (server-local time). It mirrors
// cleanup.Scheduler's Run(ctx) shape so main can start both the same way.
type Scheduler struct {
	spec   string
	job    Job
	logger *slog.Logger
}

// NewScheduler validates the cron spec up front so a malformed schedule fails
// fast at startup rather than silently never firing. spec is a standard 5-field
// cron expression (e.g. "0 9 * * *").
func NewScheduler(spec string, job Job, logger *slog.Logger) (*Scheduler, error) {
	if _, err := cron.ParseStandard(spec); err != nil {
		return nil, fmt.Errorf("digest: invalid schedule %q: %w", spec, err)
	}
	return &Scheduler{spec: spec, job: job, logger: logger}, nil
}

// Run starts the cron loop and blocks until ctx is cancelled, then waits for an
// in-flight run to finish before returning. Always returns nil (the signature
// stays error-returning to compose with other long-running goroutines).
func (s *Scheduler) Run(ctx context.Context) error {
	c := cron.New()
	if _, err := c.AddFunc(s.spec, func() {
		if err := s.job.Report(ctx); err != nil {
			s.logger.Error("stuck-pr digest run failed", slog.Any("err", err))
		}
	}); err != nil {
		// Unreachable in practice: NewScheduler already validated the spec.
		return fmt.Errorf("digest: schedule %q: %w", s.spec, err)
	}

	c.Start()
	<-ctx.Done()
	<-c.Stop().Done()
	return nil
}
