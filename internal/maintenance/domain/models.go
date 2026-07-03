package domain

import (
	"log/slog"
	"time"
)

// CleanerParams bundles everything the stale-message cleaner needs. TTL is the
// maximum age a row may reach before it becomes eligible for deletion;
// Interval is the cadence between passes (use the Interval constant in
// production); Now supplies the clock (time.Now in production, a fixed clock in
// tests).
type CleanerParams struct {
	Deleter  StaleMessageDeleter
	TTL      time.Duration
	Interval time.Duration
	Logger   *slog.Logger
	Now      func() time.Time
}
