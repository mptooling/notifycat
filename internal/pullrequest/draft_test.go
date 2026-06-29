package pullrequest_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/mptooling/notifycat/internal/pullrequest"
	storepkg "github.com/mptooling/notifycat/internal/store"
)

func draftEvent(repo string, prNumber int) pullrequest.Event {
	return pullrequest.Event{
		GitHubEvent: "pull_request",
		Action:      "converted_to_draft",
		Repository:  repo,
		PR:          pullrequest.PR{Number: prNumber},
	}
}

func TestDraftHandler_Applicable(t *testing.T) {
	h := pullrequest.NewDraftHandler(newFakePRStore(), &fakeMessenger{}, discardLogger())

	if !h.Applicable(pullrequest.Event{Action: "converted_to_draft"}) {
		t.Error("converted_to_draft should be applicable")
	}
	if h.Applicable(pullrequest.Event{Action: "opened"}) {
		t.Error("opened should not be applicable")
	}
}

func TestDraftHandler_Handle_DeletesMessageAndRow(t *testing.T) {
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	client := &fakeMessenger{}
	h := pullrequest.NewDraftHandler(st, client, discardLogger())

	if err := h.Handle(context.Background(), draftEvent("octo/widget", 42)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !containsMethod(client.methods(), "DeleteMessage") {
		t.Errorf("DeleteMessage not called: %v", client.methods())
	}
	if _, err := st.Messages(context.Background(), "octo/widget", 42); err != storepkg.ErrNotFound {
		t.Errorf("store row not deleted: err = %v", err)
	}
}

func TestDraftHandler_Handle_NoStoredMessageIsNoop(t *testing.T) {
	st := newFakePRStore()
	client := &fakeMessenger{}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := pullrequest.NewDraftHandler(st, client, logger)

	e := pullrequest.Event{
		GitHubEvent: "pull_request",
		Action:      "converted_to_draft",
		Repository:  "octo/widget",
		PR:          pullrequest.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 0 {
		t.Errorf("Slack called when no message stored: %v", client.methods())
	}

	rec := decodeLog(t, buf.Bytes())
	wantFields(t, rec, map[string]any{
		"level":        "INFO",
		"msg":          "ignored webhook event",
		"reason":       "no_stored_message",
		"handler":      "draft",
		"github_event": "pull_request",
		"action":       "converted_to_draft",
		"repository":   "octo/widget",
		"pr":           float64(42),
	})
}

func TestDraftHandler_DeletesEveryMessageAndRow(t *testing.T) {
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "acme/web", 7, "C0A", "100.1")
	_ = st.AddMessage(context.Background(), "acme/web", 7, "C0B", "200.1")
	client := &fakeMessenger{}
	h := pullrequest.NewDraftHandler(st, client, discardLogger())

	if err := h.Handle(context.Background(), draftEvent("acme/web", 7)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if client.deletes() != 2 {
		t.Fatalf("want 2 deletes; got %d", client.deletes())
	}
	if _, err := st.Messages(context.Background(), "acme/web", 7); err != storepkg.ErrNotFound {
		t.Fatalf("PR row should be deleted; got %v", err)
	}
}
