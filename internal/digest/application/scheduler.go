package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/mptooling/notifycat/internal/digest/domain"
)

// Scheduler runs one cron per distinct schedule spec, calling its job for each
// tick with the matching spec. It mirrors the maintenance cleaner's Run(ctx)
// shape so the composition root can start both the same way.
type Scheduler struct {
	specs  []string
	job    domain.ScheduleJob
	logger *slog.Logger
	tz     *time.Location
}

// NewScheduler validates every cron spec up front so a malformed schedule fails
// fast at startup rather than silently never firing. Specs is a list of standard
// 5-field cron expressions (e.g. ["0 9 * * *", "0 18 * * *"]); TZ is the
// timezone every spec is interpreted in (nil defaults to UTC).
func NewScheduler(params domain.SchedulerParams) (*Scheduler, error) {
	for _, spec := range params.Specs {
		if _, err := cron.ParseStandard(spec); err != nil {
			return nil, fmt.Errorf("digest: invalid schedule %q: %w", spec, err)
		}
	}
	tz := params.TZ
	if tz == nil {
		tz = time.UTC
	}
	return &Scheduler{specs: params.Specs, job: params.Job, logger: params.Logger, tz: tz}, nil
}

// Run starts the cron loop with one entry per spec and blocks until ctx is
// cancelled, then waits for in-flight runs to finish before returning. Always
// returns nil (the signature stays error-returning to compose with other
// long-running goroutines).
func (s *Scheduler) Run(ctx context.Context) error {
	scheduler := cron.New(cron.WithLocation(s.tz))
	for _, spec := range s.specs {
		spec := spec
		if _, err := scheduler.AddFunc(spec, func() {
			if err := s.job.ReportSchedule(ctx, spec); err != nil {
				s.logger.Error("stuck-pr digest run failed", slog.String("schedule", spec), slog.Any("err", err))
			}
		}); err != nil {
			return fmt.Errorf("digest: schedule %q: %w", spec, err)
		}
	}

	scheduler.Start()
	<-ctx.Done()
	<-scheduler.Stop().Done()
	return nil
}

var _ domain.DigestScheduler = (*Scheduler)(nil)
