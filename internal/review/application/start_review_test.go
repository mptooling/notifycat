package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/review/application"
	"github.com/mptooling/notifycat/internal/review/domain"
)

func sampleCommand() domain.StartReviewCommand {
	return domain.StartReviewCommand{
		Repository: "octo/web",
		PRNumber:   42,
		Reviewer:   domain.Reviewer{UserID: "U1", UserName: "alice"},
		Message:    domain.MessageRef{Channel: "C1", TS: "111.222", RawBlocks: []byte(`[{"type":"section"}]`), Fallback: "PR #42"},
	}
}

func newHandler(recorder *fakeRecorder, messages *fakeMessageChecker, decorator *fakeDecorator) *application.Handler {
	return application.NewHandler(domain.HandlerParams{
		Recorder:  recorder,
		Messages:  messages,
		Decorator: decorator,
		Logger:    discardLogger(),
		Now:       func() time.Time { return time.Time{} },
	})
}

func TestHandle_HappyPath_RecordsAndAppendsMarker(t *testing.T) {
	recorder := &fakeRecorder{}
	decorator := &fakeDecorator{}
	h := newHandler(recorder, &fakeMessageChecker{has: true}, decorator)

	if err := h.Handle(context.Background(), sampleCommand()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(recorder.started) != 1 {
		t.Fatalf("recorded %d reviews; want 1", len(recorder.started))
	}
	got := recorder.started[0]
	if got.repository != "octo/web" || got.prNumber != 42 || got.userID != "U1" || got.userName != "alice" {
		t.Errorf("Start = %+v; want octo/web #42 U1 alice", got)
	}
	if len(decorator.calls) != 1 {
		t.Fatalf("decorated %d times; want 1", len(decorator.calls))
	}
	if decorator.calls[0].reviewer.UserID != "U1" || decorator.calls[0].message.Channel != "C1" || decorator.calls[0].message.TS != "111.222" {
		t.Errorf("decorate call = %+v; want reviewer U1 on C1/111.222", decorator.calls[0])
	}
}

func TestHandle_DuplicateAppLevel_NoOp(t *testing.T) {
	recorder := &fakeRecorder{active: true}
	decorator := &fakeDecorator{}
	h := newHandler(recorder, &fakeMessageChecker{has: true}, decorator)

	if err := h.Handle(context.Background(), sampleCommand()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(recorder.started) != 0 {
		t.Errorf("recorded %d reviews on a duplicate; want 0", len(recorder.started))
	}
	if len(decorator.calls) != 0 {
		t.Errorf("decorated %d times on a duplicate; want 0", len(decorator.calls))
	}
}

func TestHandle_DuplicateDBRace_NoOp(t *testing.T) {
	recorder := &fakeRecorder{startErr: domain.ErrActiveReviewExists}
	decorator := &fakeDecorator{}
	h := newHandler(recorder, &fakeMessageChecker{has: true}, decorator)

	if err := h.Handle(context.Background(), sampleCommand()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(decorator.calls) != 0 {
		t.Errorf("decorated %d times on a DB race; want 0", len(decorator.calls))
	}
}

func TestHandle_UnknownMessage_Ignored(t *testing.T) {
	recorder := &fakeRecorder{}
	decorator := &fakeDecorator{}
	h := newHandler(recorder, &fakeMessageChecker{has: false}, decorator)

	if err := h.Handle(context.Background(), sampleCommand()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(recorder.started) != 0 || len(decorator.calls) != 0 {
		t.Errorf("acted on an untracked PR: started=%d decorated=%d; want 0/0", len(recorder.started), len(decorator.calls))
	}
}

func TestHandle_UpdateFailure_Swallowed(t *testing.T) {
	recorder := &fakeRecorder{}
	decorator := &fakeDecorator{err: errors.New("slack down")}
	h := newHandler(recorder, &fakeMessageChecker{has: true}, decorator)

	if err := h.Handle(context.Background(), sampleCommand()); err != nil {
		t.Fatalf("Handle should swallow a decorate failure; got %v", err)
	}
	if len(recorder.started) != 1 {
		t.Errorf("review should still be recorded despite decorate failure; started=%d", len(recorder.started))
	}
}
