package infrastructure

import (
	"time"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

// SystemClock is the wall-clock Clock.
type SystemClock struct{}

var _ domain.Clock = (*SystemClock)(nil)

// NewSystemClock returns a SystemClock.
func NewSystemClock() *SystemClock { return &SystemClock{} }

// Now implements Clock.
func (*SystemClock) Now() time.Time { return time.Now().UTC() }
