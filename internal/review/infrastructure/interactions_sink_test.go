package infrastructure

import (
	"context"
	"encoding/json"
	"testing"

	reviewdomain "github.com/mptooling/notifycat/internal/review/domain"
)

type fakeStartReview struct {
	called  bool
	command reviewdomain.StartReviewCommand
	err     error
}

func (f *fakeStartReview) Handle(_ context.Context, command reviewdomain.StartReviewCommand) error {
	f.called = true
	f.command = command
	return f.err
}

func TestStartReviewSink_HappyPath_ForwardsCommand(t *testing.T) {
	fake := &fakeStartReview{}
	sink := NewStartReviewSink(fake, discardLogger())

	rawBlocks := json.RawMessage(`[{"type":"section"}]`)
	interaction := Interaction{
		Type:    "block_actions",
		User:    User{ID: "U1", Username: "ada"},
		Channel: Channel{ID: "C1"},
		Message: Message{TS: "1.1", Text: "please review", RawBlocks: rawBlocks},
		Actions: []Action{{ActionID: "start_review", Value: "octo/web#42"}},
	}

	if err := sink(context.Background(), interaction); err != nil {
		t.Fatalf("sink: %v", err)
	}
	if !fake.called {
		t.Fatal("StartReview.Handle was not called")
	}
	cmd := fake.command
	if cmd.Repository != "octo/web" {
		t.Errorf("Repository = %q; want octo/web", cmd.Repository)
	}
	if cmd.PRNumber != 42 {
		t.Errorf("PRNumber = %d; want 42", cmd.PRNumber)
	}
	if cmd.Reviewer.UserID != "U1" || cmd.Reviewer.UserName != "ada" {
		t.Errorf("Reviewer = %+v", cmd.Reviewer)
	}
	if cmd.Message.Channel != "C1" {
		t.Errorf("Message.Channel = %q; want C1", cmd.Message.Channel)
	}
	if cmd.Message.TS != "1.1" {
		t.Errorf("Message.TS = %q; want 1.1", cmd.Message.TS)
	}
	if cmd.Message.Fallback != "please review" {
		t.Errorf("Message.Fallback = %q; want please review", cmd.Message.Fallback)
	}
	if string(cmd.Message.RawBlocks) != string(rawBlocks) {
		t.Errorf("Message.RawBlocks = %s; want %s", cmd.Message.RawBlocks, rawBlocks)
	}
}

func TestStartReviewSink_WrongType_NoOp(t *testing.T) {
	fake := &fakeStartReview{}
	sink := NewStartReviewSink(fake, discardLogger())

	interaction := Interaction{
		Type:    "shortcut",
		Actions: []Action{{ActionID: "start_review", Value: "octo/web#42"}},
	}
	if err := sink(context.Background(), interaction); err != nil {
		t.Fatalf("sink: %v", err)
	}
	if fake.called {
		t.Error("StartReview.Handle must not be called for non-block_actions type")
	}
}

func TestStartReviewSink_WrongActionID_NoOp(t *testing.T) {
	fake := &fakeStartReview{}
	sink := NewStartReviewSink(fake, discardLogger())

	interaction := Interaction{
		Type:    "block_actions",
		Actions: []Action{{ActionID: "something_else", Value: "octo/web#42"}},
	}
	if err := sink(context.Background(), interaction); err != nil {
		t.Fatalf("sink: %v", err)
	}
	if fake.called {
		t.Error("StartReview.Handle must not be called for a non-start_review action")
	}
}

func TestStartReviewSink_MalformedValue_NoOp(t *testing.T) {
	fake := &fakeStartReview{}
	sink := NewStartReviewSink(fake, discardLogger())

	interaction := Interaction{
		Type:    "block_actions",
		Actions: []Action{{ActionID: "start_review", Value: "no-hash"}},
	}
	if err := sink(context.Background(), interaction); err != nil {
		t.Fatalf("sink: %v", err)
	}
	if fake.called {
		t.Error("StartReview.Handle must not be called for a malformed value")
	}
}
