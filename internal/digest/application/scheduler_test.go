package application

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/digest/domain"
)

func schedulerDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeScheduleJob struct {
	specsCalled []string
}

func (f *fakeScheduleJob) ReportSchedule(_ context.Context, spec string) error {
	f.specsCalled = append(f.specsCalled, spec)
	return nil
}

func newScheduler(specs []string, tz *time.Location) (*Scheduler, error) {
	return NewScheduler(domain.SchedulerParams{Specs: specs, Job: &fakeScheduleJob{}, Logger: schedulerDiscardLogger(), TZ: tz})
}

func TestNewScheduler_RejectsInvalidSpec(t *testing.T) {
	if _, err := newScheduler([]string{"not-a-cron-spec"}, time.UTC); err == nil {
		t.Fatal("expected an error for an invalid cron spec, got nil")
	}
}

func TestNewScheduler_AcceptsValidSpecs(t *testing.T) {
	s, err := newScheduler([]string{"0 9 * * *", "0 18 * * *"}, time.UTC)
	if err != nil {
		t.Fatalf("valid specs rejected: %v", err)
	}
	if s == nil {
		t.Fatal("nil scheduler for valid specs")
	}
}

func TestNewScheduler_RejectsBadSpecAmongMany(t *testing.T) {
	if _, err := newScheduler([]string{"0 9 * * *", "bad-spec", "0 18 * * *"}, time.UTC); err == nil {
		t.Fatal("expected an error when one spec is invalid, got nil")
	}
}

func TestNewScheduler_StoresTimezone(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	s, err := newScheduler([]string{"0 9 * * *"}, ny)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if s.tz != ny {
		t.Errorf("scheduler tz = %v; want America/New_York (it is passed to cron.WithLocation)", s.tz)
	}
}

func TestScheduler_Run_StopsOnContextCancel(t *testing.T) {
	s, err := newScheduler([]string{"0 9 * * *"}, time.UTC)
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

type fakeCalendar struct {
	reason string
	skip   bool
	calls  int
}

func (f *fakeCalendar) SkipReason(_ time.Time) (string, bool) {
	f.calls++
	return f.reason, f.skip
}

func (f *fakeCalendar) HolidayName(_ time.Time) (string, bool) { return "", false }

func (f *fakeCalendar) Country() string { return "DE" }

func TestScheduler_SkipsTickWhenCalendarSaysSo(t *testing.T) {
	job := &fakeScheduleJob{}
	calendar := &fakeCalendar{reason: domain.SkipReasonWeekend, skip: true}
	scheduler, err := NewScheduler(domain.SchedulerParams{
		Specs:    []string{"0 9 * * *"},
		Job:      job,
		Logger:   schedulerDiscardLogger(),
		TZ:       time.UTC,
		Calendar: calendar,
		Now:      func() time.Time { return time.Date(2026, time.July, 4, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	scheduler.runTick(context.Background(), "0 9 * * *")
	if len(job.specsCalled) != 0 {
		t.Fatalf("job ran on a skipped tick: %v", job.specsCalled)
	}
	if calendar.calls != 1 {
		t.Fatalf("calendar consulted %d times; want 1", calendar.calls)
	}
}

func TestScheduler_RunsTickWhenCalendarAllows(t *testing.T) {
	job := &fakeScheduleJob{}
	scheduler, err := NewScheduler(domain.SchedulerParams{
		Specs:    []string{"0 9 * * *"},
		Job:      job,
		Logger:   schedulerDiscardLogger(),
		TZ:       time.UTC,
		Calendar: &fakeCalendar{},
		Now:      func() time.Time { return time.Date(2026, time.July, 7, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	scheduler.runTick(context.Background(), "0 9 * * *")
	if len(job.specsCalled) != 1 || job.specsCalled[0] != "0 9 * * *" {
		t.Fatalf("job calls = %v; want one call with the firing spec", job.specsCalled)
	}
}

func TestScheduler_NilCalendarRunsEveryTick(t *testing.T) {
	job := &fakeScheduleJob{}
	scheduler, err := NewScheduler(domain.SchedulerParams{
		Specs:  []string{"0 9 * * *"},
		Job:    job,
		Logger: schedulerDiscardLogger(),
		TZ:     time.UTC,
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	scheduler.runTick(context.Background(), "0 9 * * *")
	if len(job.specsCalled) != 1 {
		t.Fatalf("job calls = %v; want one call with no calendar configured", job.specsCalled)
	}
}

// The calendar must be handed the tick time in the digest timezone, not the
// clock's own zone: 23:00 UTC on Friday is Saturday in Kyiv.
func TestScheduler_EvaluatesCalendarInDigestTimezone(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatalf("load Europe/Kyiv: %v", err)
	}
	var seen time.Time
	recorder := &recordingCalendar{onCall: func(now time.Time) { seen = now }}
	scheduler, err := NewScheduler(domain.SchedulerParams{
		Specs:    []string{"0 9 * * *"},
		Job:      &fakeScheduleJob{},
		Logger:   schedulerDiscardLogger(),
		TZ:       kyiv,
		Calendar: recorder,
		Now:      func() time.Time { return time.Date(2026, time.July, 3, 23, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	scheduler.runTick(context.Background(), "0 9 * * *")
	if seen.Location().String() != "Europe/Kyiv" {
		t.Fatalf("calendar saw location %s; want Europe/Kyiv", seen.Location())
	}
	if seen.Weekday() != time.Saturday {
		t.Fatalf("calendar saw %s; 23:00 UTC Friday is Saturday in Kyiv", seen.Weekday())
	}
}

type recordingCalendar struct {
	onCall func(time.Time)
}

func (r *recordingCalendar) SkipReason(now time.Time) (string, bool) {
	r.onCall(now)
	return "", false
}

func (r *recordingCalendar) HolidayName(_ time.Time) (string, bool) { return "", false }

func (r *recordingCalendar) Country() string { return "" }

// The "skipped digest" line is an operator-facing contract documented in
// docs/digest.md and grepped in docs/troubleshooting.md. Pin its fields.
func TestScheduler_SkipLogFields(t *testing.T) {
	cases := []struct {
		name     string
		calendar domain.DigestCalendar
		now      time.Time
		want     []string
	}{
		{
			name:     "weekend",
			calendar: &fakeCalendar{reason: domain.SkipReasonWeekend, skip: true},
			now:      time.Date(2026, time.July, 4, 9, 0, 0, 0, time.UTC),
			want:     []string{`msg="skipped digest"`, `schedule="0 9 * * *"`, "reason=weekend", "date=2026-07-04", "weekday=Saturday"},
		},
		{
			name:     "holiday",
			calendar: &namedHolidayCalendar{},
			now:      time.Date(2026, time.December, 25, 9, 0, 0, 0, time.UTC),
			want:     []string{`msg="skipped digest"`, "reason=holiday", "date=2026-12-25", "country=DE", `holiday="1. Weihnachtstag"`},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var buf bytes.Buffer
			scheduler, err := NewScheduler(domain.SchedulerParams{
				Specs:    []string{"0 9 * * *"},
				Job:      &fakeScheduleJob{},
				Logger:   slog.New(slog.NewTextHandler(&buf, nil)),
				TZ:       time.UTC,
				Calendar: testCase.calendar,
				Now:      func() time.Time { return testCase.now },
			})
			if err != nil {
				t.Fatalf("NewScheduler: %v", err)
			}
			scheduler.runTick(context.Background(), "0 9 * * *")
			for _, field := range testCase.want {
				if !strings.Contains(buf.String(), field) {
					t.Errorf("log line missing %s\ngot: %s", field, buf.String())
				}
			}
		})
	}
}

type namedHolidayCalendar struct{}

func (namedHolidayCalendar) SkipReason(time.Time) (string, bool) {
	return domain.SkipReasonHoliday, true
}
func (namedHolidayCalendar) HolidayName(time.Time) (string, bool) { return "1. Weihnachtstag", true }
func (namedHolidayCalendar) Country() string                      { return "DE" }
