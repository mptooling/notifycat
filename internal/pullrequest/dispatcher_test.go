package pullrequest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/mptooling/notifycat/internal/pullrequest"
)

type fakeHandler struct {
	applicable func(pullrequest.Event) bool
	handle     func(context.Context, pullrequest.Event) error
	called     int
}

func (h *fakeHandler) Applicable(e pullrequest.Event) bool { return h.applicable(e) }
func (h *fakeHandler) Handle(ctx context.Context, e pullrequest.Event) error {
	h.called++
	return h.handle(ctx, e)
}

func TestDispatcher_RunsFirstApplicableHandler(t *testing.T) {
	skip := &fakeHandler{
		applicable: func(pullrequest.Event) bool { return false },
		handle:     func(context.Context, pullrequest.Event) error { t.Fatal("skip handle called"); return nil },
	}
	match := &fakeHandler{
		applicable: func(pullrequest.Event) bool { return true },
		handle:     func(context.Context, pullrequest.Event) error { return nil },
	}
	other := &fakeHandler{
		applicable: func(pullrequest.Event) bool { return true },
		handle:     func(context.Context, pullrequest.Event) error { t.Fatal("other handle called"); return nil },
	}

	d := pullrequest.NewDispatcher(discardLogger(), skip, match, other)
	if err := d.Dispatch(context.Background(), pullrequest.Event{Action: "opened"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if match.called != 1 {
		t.Errorf("match.called = %d; want 1", match.called)
	}
}

func TestDispatcher_NoApplicableHandlerIsNotError(t *testing.T) {
	skip := &fakeHandler{
		applicable: func(pullrequest.Event) bool { return false },
		handle:     func(context.Context, pullrequest.Event) error { return nil },
	}

	d := pullrequest.NewDispatcher(discardLogger(), skip)
	if err := d.Dispatch(context.Background(), pullrequest.Event{}); err != nil {
		t.Fatalf("Dispatch (no match): %v", err)
	}
}

func TestDispatcher_PropagatesHandlerError(t *testing.T) {
	want := errors.New("boom")
	h := &fakeHandler{
		applicable: func(pullrequest.Event) bool { return true },
		handle:     func(context.Context, pullrequest.Event) error { return want },
	}

	d := pullrequest.NewDispatcher(discardLogger(), h)
	if err := d.Dispatch(context.Background(), pullrequest.Event{}); !errors.Is(err, want) {
		t.Fatalf("Dispatch error = %v; want %v", err, want)
	}
}

func TestDispatcher_NoApplicableHandlerLogsContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	skip := &fakeHandler{
		applicable: func(pullrequest.Event) bool { return false },
		handle:     func(context.Context, pullrequest.Event) error { return nil },
	}
	d := pullrequest.NewDispatcher(logger, skip)
	if err := d.Dispatch(context.Background(), pullrequest.Event{
		GitHubEvent: "pull_request",
		Action:      "labeled",
		Repository:  "octo/widget",
		PR:          pullrequest.PR{Number: 42},
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

func decodeLog(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	rec := map[string]any{}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("decode log: %v (raw=%q)", err, raw)
	}
	return rec
}

func wantFields(t *testing.T, rec map[string]any, fields map[string]any) {
	t.Helper()
	for k, v := range fields {
		if rec[k] != v {
			t.Errorf("log[%q] = %v (%T); want %v (%T)", k, rec[k], rec[k], v, v)
		}
	}
}
