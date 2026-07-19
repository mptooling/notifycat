package application

import (
	"sync"
	"time"
)

// circuitBreaker opens after threshold consecutive gateway failures and stays
// open for the cooldown; the first call after the cooldown acts as the
// half-open probe (a success resets, a failure re-opens). Concurrent probes
// after the cooldown are possible and acceptable — the guard is against
// hammering a dead provider, not exact single-flight.
type circuitBreaker struct {
	mu        sync.Mutex
	threshold int
	cooldown  time.Duration
	failures  int
	openedAt  time.Time
}

func newCircuitBreaker(threshold int, cooldown time.Duration) *circuitBreaker {
	return &circuitBreaker{threshold: threshold, cooldown: cooldown}
}

func (b *circuitBreaker) open(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures >= b.threshold && now.Sub(b.openedAt) < b.cooldown
}

func (b *circuitBreaker) record(failed bool, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !failed {
		b.failures = 0
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.openedAt = now
	}
}
