package cleanup_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/cleanup"
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

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func bufferLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func TestScheduler_RunsOnceImmediately_ThenOnInterval(t *testing.T) {
	d := &fakeDeleter{}
	ttl := 30 * 24 * time.Hour
	interval := 20 * time.Millisecond
	s := cleanup.NewScheduler(d, ttl, interval, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx)
		close(done)
	}()

	// Immediate tick should land well within the interval.
	deadline := time.After(interval / 2)
	for d.callCount() == 0 {
		select {
		case <-deadline:
			t.Fatalf("first cleanup did not run within %v", interval/2)
		case <-time.After(1 * time.Millisecond):
		}
	}

	// Wait long enough for several more interval ticks.
	time.Sleep(interval * 4)
	cancel()
	<-done

	if got := d.callCount(); got < 3 {
		t.Fatalf("DeleteStaleBefore called %d times; want >=3 (immediate + ticks)", got)
	}
	for i, c := range d.cutoffs {
		if c.IsZero() {
			t.Errorf("call %d: cutoff is zero", i)
		}
	}
}

func TestScheduler_LogsAndContinuesOnError(t *testing.T) {
	d := &fakeDeleter{err: errors.New("boom"), errOnce: true}
	logger, buf := bufferLogger()
	interval := 20 * time.Millisecond
	s := cleanup.NewScheduler(d, 24*time.Hour, interval, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx)
		close(done)
	}()

	// Wait for at least two calls (the first errors, the second should succeed).
	deadline := time.After(interval * 5)
	for d.callCount() < 2 {
		select {
		case <-deadline:
			t.Fatalf("expected at least 2 calls, got %d", d.callCount())
		case <-time.After(2 * time.Millisecond):
		}
	}
	cancel()
	<-done

	if !bytes.Contains(buf.Bytes(), []byte("boom")) {
		t.Errorf("expected error to be logged; logs: %s", buf.String())
	}
}

func TestScheduler_StopsOnContextCancel(t *testing.T) {
	d := &fakeDeleter{}
	s := cleanup.NewScheduler(d, 24*time.Hour, 1*time.Hour, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// Allow the immediate cleanup to fire, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestScheduler_CutoffEqualsNowMinusTTL(t *testing.T) {
	d := &fakeDeleter{}
	ttl := 48 * time.Hour
	fixedNow := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	s := cleanup.NewScheduler(d, ttl, 1*time.Hour, discardLogger())
	s.SetNowFunc(func() time.Time { return fixedNow })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx)
		close(done)
	}()

	// Wait for the immediate tick.
	deadline := time.After(500 * time.Millisecond)
	for d.callCount() == 0 {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("immediate cleanup did not run")
		case <-time.After(1 * time.Millisecond):
		}
	}
	cancel()
	<-done

	wantCutoff := fixedNow.Add(-ttl)
	if !d.cutoffs[0].Equal(wantCutoff) {
		t.Errorf("cutoff = %v; want %v (now - ttl)", d.cutoffs[0], wantCutoff)
	}
}
