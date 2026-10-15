package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/mptooling/notifycat/internal/notification/domain"
)

// ScheduledRunner runs one cron per distinct send spec of the scheduled
// notification strategy.
type ScheduledRunner struct {
	specs    []string
	sender   domain.ScheduledSender
	calendar domain.ScheduleCalendar
	tz       *time.Location
	logger   *slog.Logger
	now      func() time.Time
}

// NewScheduledRunner validates every spec up front, so a bad schedule fails
// startup instead of never firing.
func NewScheduledRunner(params domain.ScheduledRunnerParams) (*ScheduledRunner, error) {
	for _, spec := range params.Specs {
		if _, err := cron.ParseStandard(spec); err != nil {
			return nil, fmt.Errorf("notification: invalid schedule %q: %w", spec, err)
		}
	}
	tz := params.TZ
	if tz == nil {
		tz = time.UTC
	}
	now := params.Now
	if now == nil {
		now = time.Now
	}
	return &ScheduledRunner{
		specs:    params.Specs,
		sender:   params.Sender,
		calendar: params.Calendar,
		tz:       tz,
		logger:   params.Logger,
		now:      now,
	}, nil
}

func (r *ScheduledRunner) runTick(ctx context.Context, spec string) {
	now := r.now().In(r.tz)
	if r.calendar != nil {
		if reason, skip := r.calendar.SkipReason(now); skip {
			r.logger.Info("skipped scheduled notifications",
				slog.String("schedule", spec),
				slog.String("reason", reason),
				slog.String("date", now.Format(time.DateOnly)))
			return
		}
	}
	if err := r.sender.SendSchedule(ctx, spec); err != nil {
		r.logger.Error("scheduled notification run failed", slog.String("schedule", spec), slog.Any("err", err))
	}
}

// Run blocks until ctx is cancelled, then waits for in-flight sends.
func (r *ScheduledRunner) Run(ctx context.Context) error {
	scheduler := cron.New(cron.WithLocation(r.tz))
	for _, spec := range r.specs {
		if _, err := scheduler.AddFunc(spec, func() { r.runTick(ctx, spec) }); err != nil {
			return fmt.Errorf("notification: schedule %q: %w", spec, err)
		}
	}

	scheduler.Start()
	<-ctx.Done()
	<-scheduler.Stop().Done()
	return nil
}

var _ domain.ScheduledRunner = (*ScheduledRunner)(nil)
