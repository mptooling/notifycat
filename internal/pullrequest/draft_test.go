package pullrequest_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/mptooling/notifycat/internal/pullrequest"
	"github.com/mptooling/notifycat/internal/store"
)

func newDraftHandler(t *testing.T, msgs *fakeSlackMessages, mappings *fakeRepoMappings, client *fakeSlackClient) *pullrequest.DraftHandler {
	t.Helper()
	return pullrequest.NewDraftHandler(
		msgs, mappings, client,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func TestDraftHandler_Applicable(t *testing.T) {
	h := newDraftHandler(t, newFakeSlackMessages(), newFakeRepoMappings(), &fakeSlackClient{})

	if !h.Applicable(pullrequest.Event{Action: "converted_to_draft"}) {
		t.Error("converted_to_draft should be applicable")
	}
	if h.Applicable(pullrequest.Event{Action: "opened"}) {
		t.Error("opened should not be applicable")
	}
}

func TestDraftHandler_Handle_DeletesMessageAndRow(t *testing.T) {
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123",
	})
	client := &fakeSlackClient{}
	h := newDraftHandler(t, msgs, mappings, client)

	e := pullrequest.Event{
		Action:     "converted_to_draft",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !containsMethod(client.methods(), "DeleteMessage") {
		t.Errorf("DeleteMessage not called: %v", client.methods())
	}
	if _, err := msgs.Get(context.Background(), "octo/widget", 42); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("store row not deleted: err = %v", err)
	}
}

func TestDraftHandler_Handle_NoStoredMessageIsNoop(t *testing.T) {
	msgs := newFakeSlackMessages()
	mappings := newFakeRepoMappings(store.RepoMapping{Repository: "octo/widget", SlackChannel: "C123"})
	client := &fakeSlackClient{}
	h := newDraftHandler(t, msgs, mappings, client)

	e := pullrequest.Event{
		Action:     "converted_to_draft",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 0 {
		t.Errorf("Slack called when no message stored: %v", client.methods())
	}
}
