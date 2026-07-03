package domain

import (
	"context"
	"time"
)

// StaleMessageDeleter deletes tracked-PR rows whose message predates a cutoff.
// It is the persistence port the cleanup use case drives; the maintenance
// infrastructure layer satisfies it over the store.
type StaleMessageDeleter interface {
	DeleteStaleBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// StaleMessageCleaner prunes stale slack_messages rows on a fixed cadence: one
// pass immediately on start, then one every Interval, until its context is
// cancelled. It deletes only database rows — never the Slack messages
// themselves. A pass's error is logged and swallowed so the next tick retries;
// Run always returns nil (so callers can compose it with other long-running
// goroutines without special-casing).
type StaleMessageCleaner interface {
	Run(ctx context.Context) error
}
