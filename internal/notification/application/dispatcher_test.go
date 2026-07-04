package application_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
)

type fakeHandler struct {
	applicable func(kernel.Event) bool
	handle     func(context.Context, kernel.Event) error
	called     int
}

func (h *fakeHandler) Applicable(event kernel.Event) bool { return h.applicable(event) }
func (h *fakeHandler) Handle(ctx context.Context, event kernel.Event) error {
	h.called++
	return h.handle(ctx, event)
}

func TestDispatcher_RunsFirstApplicableHandler(t *testing.T) {
	skip := &fakeHandler{
		applicable: func(kernel.Event) bool { return false },
		handle:     func(context.Context, kernel.Event) error { t.Fatal("skip handle called"); return nil },
	}
	match := &fakeHandler{
		applicable: func(kernel.Event) bool { return true },
		handle:     func(context.Context, kernel.Event) error { return nil },
	}
	other := &fakeHandler{
		applicable: func(kernel.Event) bool { return true },
		handle:     func(context.Context, kernel.Event) error { t.Fatal("other handle called"); return nil },
	}

	d := application.NewDispatcher(discardLogger(), []domain.Handler{skip, match, other})
	if err := d.Dispatch(context.Background(), kernel.Event{Action: kernel.ActionOpened}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if match.called != 1 {
		t.Errorf("match.called = %d; want 1", match.called)
	}
}

func TestDispatcher_NoApplicableHandlerIsNotError(t *testing.T) {
	skip := &fakeHandler{
		applicable: func(kernel.Event) bool { return false },
		handle:     func(context.Context, kernel.Event) error { return nil },
	}

	d := application.NewDispatcher(discardLogger(), []domain.Handler{skip})
	if err := d.Dispatch(context.Background(), kernel.Event{}); err != nil {
		t.Fatalf("Dispatch (no match): %v", err)
	}
}

func TestDispatcher_PropagatesHandlerError(t *testing.T) {
	want := errors.New("boom")
	h := &fakeHandler{
		applicable: func(kernel.Event) bool { return true },
		handle:     func(context.Context, kernel.Event) error { return want },
	}

	d := application.NewDispatcher(discardLogger(), []domain.Handler{h})
	if err := d.Dispatch(context.Background(), kernel.Event{}); !errors.Is(err, want) {
		t.Fatalf("Dispatch error = %v; want %v", err, want)
	}
}

func TestDispatcher_NoApplicableHandlerLogsContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	skip := &fakeHandler{
		applicable: func(kernel.Event) bool { return false },
		handle:     func(context.Context, kernel.Event) error { return nil },
	}
	d := application.NewDispatcher(logger, []domain.Handler{skip})
	if err := d.Dispatch(context.Background(), kernel.Event{
		GitHubEvent: kernel.EventPullRequest,
		Action:      "labeled",
		Repository:  "octo/widget",
		PR:          kernel.PR{Number: 42},
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	rec := decodeLog(t, buf.Bytes())
	wantFields(t, rec, map[string]any{
		"level":        "DEBUG",
		"msg":          "ignored webhook event",
		"reason":       "no_handler",
		"handler":      "",
		"github_event": "pull_request",
		"action":       "labeled",
		"repository":   "octo/widget",
		"pr":           float64(42),
	})
}
