package application_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/maintenance/application"
	"github.com/mptooling/notifycat/internal/maintenance/domain"
)

type fakeDeleter struct {
	mu      sync.Mutex
	cutoffs []time.Time
	err     error
	errOnce bool
}

func (f *fakeDeleter) DeleteStaleBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cutoffs = append(f.cutoffs, cutoff)
	if f.err != nil {
		err := f.err
		if f.errOnce {
			f.err = nil
		}
		return 0, err
	}
	return 0, nil
}

func (f *fakeDeleter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.cutoffs)
}

func (f *fakeDeleter) firstCutoff() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cutoffs[0]
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func bufferLogger() (*slog.Logger, *bytes.Buffer) {
	var logged bytes.Buffer
	return slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})), &logged
}

// runCleaner starts the cleaner and returns a stop function that cancels it and
// waits for Run to return.
func runCleaner(t *testing.T, cleaner *application.Cleaner) (stop func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = cleaner.Run(ctx)
		close(done)
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			require.Fail(t, "Run did not return after context cancel")
		}
	}
}

// newCleaner builds a Cleaner with the default clock (time.Now).
func newCleaner(deleter domain.StaleMessageDeleter, ttl, interval time.Duration, logger *slog.Logger) *application.Cleaner {
	return application.NewCleaner(domain.CleanerParams{
		Deleter:  deleter,
		TTL:      ttl,
		Interval: interval,
		Logger:   logger,
		Now:      time.Now,
	})
}

func TestCleaner_RunsOnceImmediately_ThenOnInterval(t *testing.T) {
	deleter := &fakeDeleter{}
	interval := 20 * time.Millisecond
	stop := runCleaner(t, newCleaner(deleter, 30*24*time.Hour, interval, discardLogger()))
	defer stop()

	require.Eventually(t, func() bool { return deleter.callCount() > 0 }, interval/2, time.Millisecond,
		"the first cleanup runs immediately, not after the first tick")
	require.Eventually(t, func() bool { return deleter.callCount() >= 3 }, interval*10, time.Millisecond,
		"the cleanup repeats on the interval")

	stop()
	assert.NotContains(t, deleter.cutoffs, time.Time{}, "every cutoff is a real instant")
}

func TestCleaner_LogsAndContinuesOnError(t *testing.T) {
	deleter := &fakeDeleter{err: errors.New("boom"), errOnce: true}
	logger, logged := bufferLogger()
	interval := 20 * time.Millisecond
	stop := runCleaner(t, newCleaner(deleter, 24*time.Hour, interval, logger))
	defer stop()

	// The first call errors; the loop must keep going and call again.
	require.Eventually(t, func() bool { return deleter.callCount() >= 2 }, interval*10, 2*time.Millisecond)

	stop()
	assert.Contains(t, logged.String(), "boom")
}

func TestCleaner_StopsOnContextCancel(t *testing.T) {
	deleter := &fakeDeleter{}
	stop := runCleaner(t, newCleaner(deleter, 24*time.Hour, time.Hour, discardLogger()))

	require.Eventually(t, func() bool { return deleter.callCount() > 0 }, time.Second, time.Millisecond)

	stop()
}

func TestCleaner_CutoffEqualsNowMinusTTL(t *testing.T) {
	deleter := &fakeDeleter{}
	ttl := 48 * time.Hour
	fixedNow := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	cleaner := application.NewCleaner(domain.CleanerParams{
		Deleter:  deleter,
		TTL:      ttl,
		Interval: time.Hour,
		Logger:   discardLogger(),
		Now:      func() time.Time { return fixedNow },
	})
	stop := runCleaner(t, cleaner)
	defer stop()

	require.Eventually(t, func() bool { return deleter.callCount() > 0 }, time.Second, time.Millisecond)

	stop()
	assert.Equal(t, fixedNow.Add(-ttl), deleter.firstCutoff())
}
