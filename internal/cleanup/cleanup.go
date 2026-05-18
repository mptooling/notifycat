// Package cleanup runs the scheduled, in-process pruning of stale rows from
// the slack_messages table. The scheduler runs one cleanup immediately on
// start, then once every Interval, until its context is cancelled.
package cleanup

import (
	"context"
	"log/slog"
	"time"
)

// Interval is the fixed cadence between cleanup ticks. 24h is conservative
// enough that a transient DB error has nearly a full day to clear before the
// next attempt.
const Interval = 24 * time.Hour

// StaleMessageDeleter is the slice of the store API the scheduler needs.
// Declared here, in the consumer package, so the store stays unaware of how
// callers use it.
type StaleMessageDeleter interface {
	DeleteStaleBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// Scheduler periodically deletes slack_messages rows older than ttl.
type Scheduler struct {
	deleter  StaleMessageDeleter
	ttl      time.Duration
	interval time.Duration
	now      func() time.Time
	logger   *slog.Logger
}

// NewScheduler constructs a Scheduler. ttl is the maximum age a row may reach
// before becoming eligible for deletion; interval is the cadence between
// cleanup attempts (use cleanup.Interval in production).
func NewScheduler(deleter StaleMessageDeleter, ttl, interval time.Duration, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		deleter:  deleter,
		ttl:      ttl,
		interval: interval,
		now:      time.Now,
		logger:   logger,
	}
}

// SetNowFunc overrides the clock used to compute cutoff timestamps. Tests
// only — production code uses the default time.Now.
func (s *Scheduler) SetNowFunc(now func() time.Time) { s.now = now }

// Run drives one cleanup immediately, then one per interval, until ctx is
// cancelled. Errors from the deleter are logged and swallowed — the next tick
// retries. Always returns nil (the signature stays error-returning so callers
// can compose it with other long-running goroutines without special-casing).
func (s *Scheduler) Run(ctx context.Context) error {
	s.tick(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	cutoff := s.now().Add(-s.ttl)
	deleted, err := s.deleter.DeleteStaleBefore(ctx, cutoff)
	if err != nil {
		s.logger.Error("cleanup: delete stale slack messages",
			slog.Any("err", err),
			slog.Time("cutoff", cutoff),
		)
		return
	}
	if deleted > 0 {
		s.logger.Info("cleanup: deleted stale slack messages",
			slog.Int64("deleted", deleted),
			slog.Time("cutoff", cutoff),
		)
	}
}
