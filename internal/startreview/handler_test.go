package startreview_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/slackhook"
	"github.com/mptooling/notifycat/internal/startreview"
	"github.com/mptooling/notifycat/internal/store"
)

type fakeReviews struct {
	activeErr  error // returned by ActiveForUser (default: caller sets)
	startErr   error
	startCalls int
}

func (f *fakeReviews) ActiveForUser(_ context.Context, _ string, _ int, _ string) (store.CodeReview, error) {
	return store.CodeReview{}, f.activeErr
}
func (f *fakeReviews) Start(_ context.Context, _ string, _ int, _, _ string) error {
	f.startCalls++
	return f.startErr
}

type fakeMessages struct {
	err error
}

func (f *fakeMessages) Messages(_ context.Context, _ string, _ int) ([]store.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []store.Message{{Channel: "C1", MessageID: "1.1"}}, nil
}

type fakeUpdater struct {
	calls    int
	blocks   []json.RawMessage
	fallback string
	err      error
}

func (f *fakeUpdater) UpdateMessageRawBlocks(_ context.Context, _, _ string, blocks []json.RawMessage, fallback string) error {
	f.calls++
	f.blocks = blocks
	f.fallback = fallback
	return f.err
}

func newHandler(reviews *fakeReviews, messages *fakeMessages, updater *fakeUpdater) *startreview.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fixed := time.Date(2026, 6, 5, 14, 4, 0, 0, time.UTC)
	return startreview.NewHandler(reviews, messages, updater, slack.NewComposer(""), logger, func() time.Time { return fixed })
}

func clickWithBlocks(blocks string) slackhook.Interaction {
	return slackhook.Interaction{
		Type:    "block_actions",
		User:    slackhook.User{ID: "U1", Username: "ada"},
		Channel: slackhook.Channel{ID: "C1"},
		Message: slackhook.Message{TS: "1.1", Text: "please review", RawBlocks: json.RawMessage(blocks)},
		Actions: []slackhook.Action{{ActionID: "start_review", Value: "octo/web#42"}},
	}
}

const threeBlocks = `[{"type":"section","text":{"type":"mrkdwn","text":"hi"}},{"type":"context","elements":[{"type":"mrkdwn","text":"ctx"}]},{"type":"actions","elements":[{"type":"button","action_id":"start_review"}]}]`

func TestHandle_HappyPath_RecordsAndAppendsMarker(t *testing.T) {
	reviews := &fakeReviews{activeErr: store.ErrNotFound}
	updater := &fakeUpdater{}
	h := newHandler(reviews, &fakeMessages{}, updater)

	if err := h.Handle(context.Background(), clickWithBlocks(threeBlocks)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.startCalls != 1 {
		t.Errorf("Start calls = %d, want 1", reviews.startCalls)
	}
	if updater.calls != 1 {
		t.Fatalf("update calls = %d, want 1", updater.calls)
	}
	if len(updater.blocks) != 4 {
		t.Fatalf("blocks len = %d, want 4 (marker inserted)", len(updater.blocks))
	}
	// Marker sits before the actions block (index 2 of 4).
	if !strings.Contains(string(updater.blocks[2]), "reviewing") {
		t.Errorf("marker not before actions block; blocks[2]=%s", updater.blocks[2])
	}
	if !strings.Contains(string(updater.blocks[3]), "actions") {
		t.Errorf("actions block should be last; blocks[3]=%s", updater.blocks[3])
	}
	if updater.fallback != "please review" {
		t.Errorf("fallback = %q, want passthrough of original text", updater.fallback)
	}
}

func TestHandle_DuplicateAppLevel_NoOp(t *testing.T) {
	reviews := &fakeReviews{activeErr: nil} // already reviewing
	updater := &fakeUpdater{}
	h := newHandler(reviews, &fakeMessages{}, updater)

	if err := h.Handle(context.Background(), clickWithBlocks(threeBlocks)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.startCalls != 0 {
		t.Errorf("Start should not be called on app-level duplicate")
	}
	if updater.calls != 0 {
		t.Errorf("no update on duplicate")
	}
}

func TestHandle_DuplicateDBRace_NoOp(t *testing.T) {
	reviews := &fakeReviews{activeErr: store.ErrNotFound, startErr: store.ErrActiveReviewExists}
	updater := &fakeUpdater{}
	h := newHandler(reviews, &fakeMessages{}, updater)

	if err := h.Handle(context.Background(), clickWithBlocks(threeBlocks)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if updater.calls != 0 {
		t.Errorf("no update when DB rejects the racing insert")
	}
}

func TestHandle_UnknownMessage_Ignored(t *testing.T) {
	reviews := &fakeReviews{activeErr: store.ErrNotFound}
	updater := &fakeUpdater{}
	h := newHandler(reviews, &fakeMessages{err: store.ErrNotFound}, updater)

	if err := h.Handle(context.Background(), clickWithBlocks(threeBlocks)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.startCalls != 0 || updater.calls != 0 {
		t.Errorf("unknown message must short-circuit before recording/updating")
	}
}

func TestHandle_MalformedValue_Ignored(t *testing.T) {
	reviews := &fakeReviews{activeErr: store.ErrNotFound}
	updater := &fakeUpdater{}
	h := newHandler(reviews, &fakeMessages{}, updater)

	click := clickWithBlocks(threeBlocks)
	click.Actions[0].Value = "not-a-pr"
	if err := h.Handle(context.Background(), click); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.startCalls != 0 || updater.calls != 0 {
		t.Errorf("malformed value must be ignored")
	}
}

func TestHandle_WrongAction_Ignored(t *testing.T) {
	reviews := &fakeReviews{activeErr: store.ErrNotFound}
	updater := &fakeUpdater{}
	h := newHandler(reviews, &fakeMessages{}, updater)

	click := clickWithBlocks(threeBlocks)
	click.Actions[0].ActionID = "something_else"
	if err := h.Handle(context.Background(), click); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.startCalls != 0 || updater.calls != 0 {
		t.Errorf("non start_review action must be ignored")
	}
}

func TestHandle_UpdateFailure_Swallowed(t *testing.T) {
	reviews := &fakeReviews{activeErr: store.ErrNotFound}
	updater := &fakeUpdater{err: errors.New("slack down")}
	h := newHandler(reviews, &fakeMessages{}, updater)

	if err := h.Handle(context.Background(), clickWithBlocks(threeBlocks)); err != nil {
		t.Fatalf("update failure must not surface as an error: %v", err)
	}
	if reviews.startCalls != 1 {
		t.Errorf("review should still be recorded even if the update fails")
	}
}
