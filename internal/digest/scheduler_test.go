package digest

import (
	"context"
	"testing"
	"time"
)

type stubJob struct{}

func (stubJob) Report(context.Context) error { return nil }

func TestNewScheduler_RejectsInvalidSpec(t *testing.T) {
	if _, err := NewScheduler("not-a-cron-spec", stubJob{}, discardLogger()); err == nil {
		t.Fatal("expected an error for an invalid cron spec, got nil")
	}
}

func TestNewScheduler_AcceptsValidSpec(t *testing.T) {
	s, err := NewScheduler("0 9 * * *", stubJob{}, discardLogger())
	if err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	if s == nil {
		t.Fatal("nil scheduler for valid spec")
	}
}

func TestScheduler_Run_StopsOnContextCancel(t *testing.T) {
	s, err := NewScheduler("0 9 * * *", stubJob{}, discardLogger())
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v; want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
