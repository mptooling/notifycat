package digest

import (
	"context"
	"testing"
	"time"
)

type fakeScheduleJob struct {
	specsCalled []string
}

func (f *fakeScheduleJob) ReportSchedule(_ context.Context, spec string) error {
	f.specsCalled = append(f.specsCalled, spec)
	return nil
}

func TestNewScheduler_RejectsInvalidSpec(t *testing.T) {
	if _, err := NewScheduler([]string{"not-a-cron-spec"}, &fakeScheduleJob{}, discardLogger(), time.UTC); err == nil {
		t.Fatal("expected an error for an invalid cron spec, got nil")
	}
}

func TestNewScheduler_AcceptsValidSpecs(t *testing.T) {
	specs := []string{"0 9 * * *", "0 18 * * *"}
	s, err := NewScheduler(specs, &fakeScheduleJob{}, discardLogger(), time.UTC)
	if err != nil {
		t.Fatalf("valid specs rejected: %v", err)
	}
	if s == nil {
		t.Fatal("nil scheduler for valid specs")
	}
}

func TestNewScheduler_RejectsBadSpecAmongMany(t *testing.T) {
	specs := []string{"0 9 * * *", "bad-spec", "0 18 * * *"}
	if _, err := NewScheduler(specs, &fakeScheduleJob{}, discardLogger(), time.UTC); err == nil {
		t.Fatal("expected an error when one spec is invalid, got nil")
	}
}

func TestNewScheduler_StoresTimezone(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	s, err := NewScheduler([]string{"0 9 * * *"}, &fakeScheduleJob{}, discardLogger(), ny)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if s.tz != ny {
		t.Errorf("scheduler tz = %v; want America/New_York (it is passed to cron.WithLocation)", s.tz)
	}
}

func TestScheduler_Run_StopsOnContextCancel(t *testing.T) {
	s, err := NewScheduler([]string{"0 9 * * *"}, &fakeScheduleJob{}, discardLogger(), time.UTC)
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
