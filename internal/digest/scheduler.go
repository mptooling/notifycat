package digest

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
)

// ScheduleJob is the unit the Scheduler fires on each cron tick. *Reporter satisfies it.
type ScheduleJob interface {
	ReportSchedule(ctx context.Context, spec string) error
}

// Scheduler runs one cron per distinct schedule spec, calling its job for each
// tick with the matching spec. It mirrors cleanup.Scheduler's Run(ctx) shape
// so main can start both the same way.
type Scheduler struct {
	specs  []string
	job    ScheduleJob
	logger *slog.Logger
	tz     *time.Location
}

// NewScheduler validates every cron spec up front so a malformed schedule fails
// fast at startup rather than silently never firing. specs is a list of
// standard 5-field cron expressions (e.g. ["0 9 * * *", "0 18 * * *"]); tz is
// the timezone every spec is interpreted in (nil defaults to UTC).
func NewScheduler(specs []string, job ScheduleJob, logger *slog.Logger, tz *time.Location) (*Scheduler, error) {
	for _, spec := range specs {
		if _, err := cron.ParseStandard(spec); err != nil {
			return nil, fmt.Errorf("digest: invalid schedule %q: %w", spec, err)
		}
	}
	if tz == nil {
		tz = time.UTC
	}
	return &Scheduler{specs: specs, job: job, logger: logger, tz: tz}, nil
}

// Run starts the cron loop with one entry per spec and blocks until ctx is
// cancelled, then waits for in-flight runs to finish before returning. Always
// returns nil (the signature stays error-returning to compose with other
// long-running goroutines).
func (s *Scheduler) Run(ctx context.Context) error {
	c := cron.New(cron.WithLocation(s.tz))
	for _, spec := range s.specs {
		spec := spec
		if _, err := c.AddFunc(spec, func() {
			if err := s.job.ReportSchedule(ctx, spec); err != nil {
				s.logger.Error("stuck-pr digest run failed", slog.String("schedule", spec), slog.Any("err", err))
			}
		}); err != nil {
			return fmt.Errorf("digest: schedule %q: %w", spec, err)
		}
	}

	c.Start()
	<-ctx.Done()
	<-c.Stop().Done()
	return nil
}
