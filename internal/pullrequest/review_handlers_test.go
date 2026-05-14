package pullrequest_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/mptooling/notifycat/internal/pullrequest"
	"github.com/mptooling/notifycat/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func setupReviewFixture(t *testing.T) (*fakeSlackMessages, *fakeRepoMappings, *fakeSlackClient) {
	t.Helper()
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123",
	})
	return msgs, mappings, &fakeSlackClient{}
}

// ----- Approve -----

func TestApproveHandler_Applicable(t *testing.T) {
	h := pullrequest.NewApproveHandler(nil, nil, nil, discardLogger(), "white_check_mark")

	if !h.Applicable(pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "approved"}}) {
		t.Error("submitted+approved should be applicable")
	}
	if h.Applicable(pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "commented"}}) {
		t.Error("submitted+commented should not be approve-applicable")
	}
	if h.Applicable(pullrequest.Event{Action: "submitted"}) {
		t.Error("submitted with no review should not be applicable")
	}
}

func TestApproveHandler_Handle_AddsReaction(t *testing.T) {
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), "white_check_mark")

	e := pullrequest.Event{
		Action:     "submitted",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42},
		Review:     &pullrequest.Review{State: "approved"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 1 || client.calls[0].Method != "AddReaction" {
		t.Fatalf("calls = %v", client.methods())
	}
	if client.calls[0].Name != "white_check_mark" {
		t.Errorf("reaction name = %q", client.calls[0].Name)
	}
}

// ----- Commented -----

func TestCommentedHandler_Applicable(t *testing.T) {
	h := pullrequest.NewCommentedHandler(nil, nil, nil, discardLogger(), "speech_balloon")

	cases := []struct {
		name string
		e    pullrequest.Event
		want bool
	}{
		{"submitted+commented", pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "commented"}}, true},
		{"edited+commented", pullrequest.Event{Action: "edited", Review: &pullrequest.Review{State: "commented"}}, true},
		{"submitted+approved", pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "approved"}}, false},
		{"submitted no review", pullrequest.Event{Action: "submitted"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := h.Applicable(c.e); got != c.want {
				t.Errorf("Applicable = %v; want %v", got, c.want)
			}
		})
	}
}

func TestCommentedHandler_Handle_AddsReaction(t *testing.T) {
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), "speech_balloon")

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR: pullrequest.PR{Number: 42}, Review: &pullrequest.Review{State: "commented"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 1 || client.calls[0].Name != "speech_balloon" {
		t.Fatalf("calls = %+v", client.calls)
	}
}

// ----- RequestChange -----

func TestRequestChangeHandler_Applicable(t *testing.T) {
	h := pullrequest.NewRequestChangeHandler(nil, nil, nil, discardLogger(), "exclamation")

	if !h.Applicable(pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "changes_requested"}}) {
		t.Error("submitted+changes_requested should be applicable")
	}
	if h.Applicable(pullrequest.Event{Action: "edited", Review: &pullrequest.Review{State: "changes_requested"}}) {
		t.Error("edited+changes_requested should not be applicable (PHP parity)")
	}
}

func TestRequestChangeHandler_Handle_AddsReaction(t *testing.T) {
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewRequestChangeHandler(msgs, mappings, client, discardLogger(), "exclamation")

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR: pullrequest.PR{Number: 42}, Review: &pullrequest.Review{State: "changes_requested"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 1 || client.calls[0].Name != "exclamation" {
		t.Fatalf("calls = %+v", client.calls)
	}
}

// Shared: when no SlackMessage exists, the reaction handlers are no-ops.
func TestReviewHandlers_NoStoredMessageIsNoop(t *testing.T) {
	mappings := newFakeRepoMappings(store.RepoMapping{Repository: "octo/widget", SlackChannel: "C123"})
	type ctor func() pullrequest.EventHandler
	cases := []struct {
		name string
		ctor ctor
		e    pullrequest.Event
	}{
		{
			name: "approve",
			e:    pullrequest.Event{Action: "submitted", Repository: "octo/widget", PR: pullrequest.PR{Number: 42}, Review: &pullrequest.Review{State: "approved"}},
		},
		{
			name: "commented",
			e:    pullrequest.Event{Action: "submitted", Repository: "octo/widget", PR: pullrequest.PR{Number: 42}, Review: &pullrequest.Review{State: "commented"}},
		},
		{
			name: "request_change",
			e:    pullrequest.Event{Action: "submitted", Repository: "octo/widget", PR: pullrequest.PR{Number: 42}, Review: &pullrequest.Review{State: "changes_requested"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msgs := newFakeSlackMessages() // empty
			client := &fakeSlackClient{}
			var h pullrequest.EventHandler
			switch c.name {
			case "approve":
				h = pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), "x")
			case "commented":
				h = pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), "x")
			case "request_change":
				h = pullrequest.NewRequestChangeHandler(msgs, mappings, client, discardLogger(), "x")
			}
			if err := h.Handle(context.Background(), c.e); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if len(client.calls) != 0 {
				t.Errorf("Slack called when no message stored: %v", client.methods())
			}
		})
	}
}
