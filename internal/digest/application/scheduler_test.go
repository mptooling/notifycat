package application

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

type recordingCalendar struct {
	seen time.Time
}

func (r *recordingCalendar) SkipReason(now time.Time) (string, bool) {
	r.seen = now
	return "", false
}

func (r *recordingCalendar) HolidayName(_ time.Time) (string, bool) { return "", false }

func (r *recordingCalendar) Country() string { return "" }

type namedHolidayCalendar struct{}

func (namedHolidayCalendar) SkipReason(time.Time) (string, bool) {
	return domain.SkipReasonHoliday, true
}

func (namedHolidayCalendar) HolidayName(time.Time) (string, bool) { return "1. Weihnachtstag", true }

func (namedHolidayCalendar) Country() string { return "DE" }

func newScheduler(specs []string, location *time.Location) (*Scheduler, error) {
	return NewScheduler(domain.SchedulerParams{
		Specs:  specs,
		Job:    &fakeScheduleJob{},
		Logger: schedulerDiscardLogger(),
		TZ:     location,
	})
}

// runTickWith builds a scheduler around the given calendar/clock and fires one tick.
func runTickWith(t *testing.T, params domain.SchedulerParams) {
	t.Helper()

	if params.Specs == nil {
		params.Specs = []string{"0 9 * * *"}
	}
	if params.Logger == nil {
		params.Logger = schedulerDiscardLogger()
	}
	scheduler, err := NewScheduler(params)
	require.NoError(t, err)
	scheduler.runTick(context.Background(), "0 9 * * *")
}

func TestNewScheduler_RejectsInvalidSpec(t *testing.T) {
	_, err := newScheduler([]string{"not-a-cron-spec"}, time.UTC)

	assert.Error(t, err)
}

func TestNewScheduler_AcceptsValidSpecs(t *testing.T) {
	scheduler, err := newScheduler([]string{"0 9 * * *", "0 18 * * *"}, time.UTC)

	require.NoError(t, err)
	assert.NotNil(t, scheduler)
}

func TestNewScheduler_RejectsBadSpecAmongMany(t *testing.T) {
	_, err := newScheduler([]string{"0 9 * * *", "bad-spec", "0 18 * * *"}, time.UTC)

	assert.Error(t, err, "one bad spec fails the whole scheduler")
}

func TestNewScheduler_StoresTimezone(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	scheduler, err := newScheduler([]string{"0 9 * * *"}, newYork)

	require.NoError(t, err)
	assert.Equal(t, newYork, scheduler.tz, "the zone is handed to cron.WithLocation")
}

func TestScheduler_Run_StopsOnContextCancel(t *testing.T) {
	scheduler, err := newScheduler([]string{"0 9 * * *"}, time.UTC)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		require.Fail(t, "Run did not return after context cancel")
	}
}

func TestScheduler_SkipsTickWhenCalendarSaysSo(t *testing.T) {
	job := &fakeScheduleJob{}
	calendar := &fakeCalendar{reason: domain.SkipReasonWeekend, skip: true}

	runTickWith(t, domain.SchedulerParams{
		Job:      job,
		TZ:       time.UTC,
		Calendar: calendar,
		Now:      func() time.Time { return time.Date(2026, time.July, 4, 9, 0, 0, 0, time.UTC) },
	})

	assert.Empty(t, job.specsCalled)
	assert.Equal(t, 1, calendar.calls, "the calendar is consulted once per tick")
}

func TestScheduler_RunsTickWhenCalendarAllows(t *testing.T) {
	job := &fakeScheduleJob{}

	runTickWith(t, domain.SchedulerParams{
		Job:      job,
		TZ:       time.UTC,
		Calendar: &fakeCalendar{},
		Now:      func() time.Time { return time.Date(2026, time.July, 7, 9, 0, 0, 0, time.UTC) },
	})

	assert.Equal(t, []string{"0 9 * * *"}, job.specsCalled)
}

func TestScheduler_NilCalendarRunsEveryTick(t *testing.T) {
	job := &fakeScheduleJob{}

	runTickWith(t, domain.SchedulerParams{Job: job, TZ: time.UTC})

	assert.Equal(t, []string{"0 9 * * *"}, job.specsCalled)
}

// The calendar must be handed the tick time in the digest timezone, not the
// clock's own zone: 23:00 UTC on Friday is Saturday in Kyiv.
func TestScheduler_EvaluatesCalendarInDigestTimezone(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)
	calendar := &recordingCalendar{}

	runTickWith(t, domain.SchedulerParams{
		Job:      &fakeScheduleJob{},
		TZ:       kyiv,
		Calendar: calendar,
		Now:      func() time.Time { return time.Date(2026, time.July, 3, 23, 0, 0, 0, time.UTC) },
	})

	assert.Equal(t, "Europe/Kyiv", calendar.seen.Location().String())
	assert.Equal(t, time.Saturday, calendar.seen.Weekday())
}

// The "skipped digest" line is an operator-facing contract documented in
// docs/digest.md and grepped in docs/troubleshooting.md. Pin its fields.
func TestScheduler_SkipLogFields(t *testing.T) {
	testCases := []struct {
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

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var logged bytes.Buffer

			runTickWith(t, domain.SchedulerParams{
				Job:      &fakeScheduleJob{},
				Logger:   slog.New(slog.NewTextHandler(&logged, nil)),
				TZ:       time.UTC,
				Calendar: testCase.calendar,
				Now:      func() time.Time { return testCase.now },
			})

			for _, field := range testCase.want {
				assert.Contains(t, logged.String(), field)
			}
		})
	}
}
