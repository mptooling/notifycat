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

	"github.com/mptooling/notifycat/internal/notification/domain"
)

type recordingSender struct {
	specs []string
}

func (r *recordingSender) SendSchedule(_ context.Context, spec string) error {
	r.specs = append(r.specs, spec)
	return nil
}

type stubCalendar struct {
	reason string
	skip   bool
	seen   time.Time
}

func (s *stubCalendar) SkipReason(now time.Time) (string, bool) {
	s.seen = now
	return s.reason, s.skip
}

func newTestRunner(t *testing.T, params domain.ScheduledRunnerParams) *ScheduledRunner {
	t.Helper()

	if params.Specs == nil {
		params.Specs = []string{"0 9 * * *"}
	}
	if params.Logger == nil {
		params.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	runner, err := NewScheduledRunner(params)
	require.NoError(t, err)
	return runner
}

func TestNewScheduledRunner_RejectsBadSpec(t *testing.T) {
	_, err := NewScheduledRunner(domain.ScheduledRunnerParams{
		Specs:  []string{"not a cron"},
		Sender: &recordingSender{},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	require.ErrorContains(t, err, `invalid schedule "not a cron"`)
}

func TestScheduledRunner_TickSendsTheSpec(t *testing.T) {
	sender := &recordingSender{}
	runner := newTestRunner(t, domain.ScheduledRunnerParams{Sender: sender, Calendar: &stubCalendar{}})

	runner.runTick(context.Background(), "0 9 * * *")

	assert.Equal(t, []string{"0 9 * * *"}, sender.specs)
}

func TestScheduledRunner_SkippedDayKeepsTheQueue(t *testing.T) {
	sender := &recordingSender{}
	buffer := &bytes.Buffer{}
	runner := newTestRunner(t, domain.ScheduledRunnerParams{
		Sender:   sender,
		Calendar: &stubCalendar{reason: "holiday", skip: true},
		Logger:   slog.New(slog.NewTextHandler(buffer, nil)),
		Now:      func() time.Time { return time.Date(2026, time.December, 25, 9, 0, 0, 0, time.UTC) },
	})

	runner.runTick(context.Background(), "0 9 * * *")

	assert.Empty(t, sender.specs, "a skipped day sends nothing")
	assert.Contains(t, buffer.String(), "skipped scheduled notifications")
	assert.Contains(t, buffer.String(), "reason=holiday")
}

func TestScheduledRunner_CalendarSeesTheConfiguredZone(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	require.NoError(t, err)
	calendar := &stubCalendar{}
	runner := newTestRunner(t, domain.ScheduledRunnerParams{
		Sender:   &recordingSender{},
		Calendar: calendar,
		TZ:       kyiv,
		Now:      func() time.Time { return time.Date(2026, time.October, 15, 22, 30, 0, 0, time.UTC) },
	})

	runner.runTick(context.Background(), "0 9 * * *")

	assert.Equal(t, kyiv, calendar.seen.Location())
	assert.Equal(t, 16, calendar.seen.Day(), "22:30 UTC is already the next day in Kyiv")
}
