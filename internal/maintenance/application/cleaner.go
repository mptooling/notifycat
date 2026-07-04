package application

import (
	"context"
	"log/slog"
	"time"

	"github.com/mptooling/notifycat/internal/maintenance/domain"
)

// Cleaner is the stale-message cleanup use case; see domain.StaleMessageCleaner.
type Cleaner struct {
	deleter  domain.StaleMessageDeleter
	ttl      time.Duration
	interval time.Duration
	now      func() time.Time
	logger   *slog.Logger
}

// NewCleaner constructs the stale-message cleanup use case from its domain params.
func NewCleaner(params domain.CleanerParams) *Cleaner {
	return &Cleaner{
		deleter:  params.Deleter,
		ttl:      params.TTL,
		interval: params.Interval,
		now:      params.Now,
		logger:   params.Logger,
	}
}

// Run implements domain.StaleMessageCleaner.
func (c *Cleaner) Run(ctx context.Context) error {
	c.tick(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

func (c *Cleaner) tick(ctx context.Context) {
	cutoff := c.now().Add(-c.ttl)
	deleted, err := c.deleter.DeleteStaleBefore(ctx, cutoff)
	if err != nil {
		c.logger.Error("cleanup: delete stale slack messages",
			slog.Any("err", err),
			slog.Time("cutoff", cutoff),
		)
		return
	}
	if deleted > 0 {
		c.logger.Info("cleanup: deleted stale slack messages",
			slog.Int64("deleted", deleted),
			slog.Time("cutoff", cutoff),
		)
	}
}
