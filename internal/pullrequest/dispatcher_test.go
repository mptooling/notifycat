package pullrequest_test

import (
	"context"
	"errors"
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

	d := pullrequest.NewDispatcher(skip, match, other)
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

	d := pullrequest.NewDispatcher(skip)
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

	d := pullrequest.NewDispatcher(h)
	if err := d.Dispatch(context.Background(), pullrequest.Event{}); !errors.Is(err, want) {
		t.Fatalf("Dispatch error = %v; want %v", err, want)
	}
}
