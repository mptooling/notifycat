package pullrequest_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/pullrequest"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

func newCloseHandler(t *testing.T, msgs *fakeSlackMessages, mappings *fakeRepoMappings, client *fakeSlackClient, reactionsEnabled bool) *pullrequest.CloseHandler {
	t.Helper()
	return pullrequest.NewCloseHandler(
		msgs, mappings, client,
		slack.NewComposer("rocket"),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		pullrequest.CloseOptions{
			ReactionsEnabled: reactionsEnabled,
			MergedEmoji:      "twisted_rightwards_arrows",
			ClosedEmoji:      "x",
		},
	)
}

func TestCloseHandler_Applicable(t *testing.T) {
	h := newCloseHandler(t, newFakeSlackMessages(), newFakeRepoMappings(), &fakeSlackClient{}, false)

	if !h.Applicable(pullrequest.Event{Action: "closed"}) {
		t.Error("closed should be applicable")
	}
	if h.Applicable(pullrequest.Event{Action: "opened"}) {
		t.Error("opened should not be applicable")
	}
}

func TestCloseHandler_Handle_UpdatesMessage(t *testing.T) {
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123", Mentions: []string{"@a"},
	})
	client := &fakeSlackClient{}
	h := newCloseHandler(t, msgs, mappings, client, true)

	e := pullrequest.Event{
		Action:     "closed",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42, Title: "fix", URL: "u", Author: "a", Merged: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if !containsMethod(client.methods(), "UpdateMessage") {
		t.Errorf("UpdateMessage not called; methods = %v", client.methods())
	}
	if !containsMethod(client.methods(), "AddReaction") {
		t.Errorf("AddReaction not called when reactions enabled: %v", client.methods())
	}
	// Find the AddReaction call and check it used the merged emoji.
	for _, c := range client.calls {
		if c.Method == "AddReaction" && c.Name != "twisted_rightwards_arrows" {
			t.Errorf("AddReaction name = %q; want twisted_rightwards_arrows", c.Name)
		}
	}
	// The updated message swaps the leading emoji to the merged reaction,
	// prepends [Merged], strikes the linked title, and keeps a context line.
	for _, c := range client.calls {
		if c.Method != "UpdateMessage" {
			continue
		}
		wantSection := ":twisted_rightwards_arrows: [Merged] ~<u|PR #42: fix>~"
		if got := sectionTextOf(c.Msg); got != wantSection {
			t.Errorf("update section = %q; want %q", got, wantSection)
		}
		if ctx := contextTextOf(c.Msg); !strings.Contains(ctx, "octo/widget · a") {
			t.Errorf("update context line missing repo/author: %q", ctx)
		}
	}
}

func TestCloseHandler_Handle_ClosedNotMergedUsesClosedEmoji(t *testing.T) {
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123",
	})
	client := &fakeSlackClient{}
	h := newCloseHandler(t, msgs, mappings, client, true)

	e := pullrequest.Event{
		Action:     "closed",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42, Merged: false},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	for _, c := range client.calls {
		if c.Method == "AddReaction" && c.Name != "x" {
			t.Errorf("AddReaction name = %q; want x", c.Name)
		}
	}
}

func TestCloseHandler_Handle_NoReactionWhenDisabled(t *testing.T) {
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{Repository: "octo/widget", SlackChannel: "C123"})
	client := &fakeSlackClient{}
	h := newCloseHandler(t, msgs, mappings, client, false)

	e := pullrequest.Event{
		Action:     "closed",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42, Merged: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if containsMethod(client.methods(), "AddReaction") {
		t.Errorf("AddReaction called when reactions disabled: %v", client.methods())
	}
}

func TestCloseHandler_Handle_MarksClosed(t *testing.T) {
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{PRNumber: 42, Repository: "octo/widget", TS: "ts1"})
	mappings := newFakeRepoMappings(store.RepoMapping{Repository: "octo/widget", SlackChannel: "C123"})
	client := &fakeSlackClient{}

	// Reactions disabled, to prove MarkClosed is independent of reactions.
	h := newCloseHandler(t, msgs, mappings, client, false)
	e := pullrequest.Event{
		Action:     "closed",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42, Merged: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(msgs.closed) != 1 || msgs.closed[0] != (fakeKey{"octo/widget", 42}) {
		t.Fatalf("MarkClosed not recorded for the PR: %v", msgs.closed)
	}
}

func TestCloseHandler_Handle_NoStoredMessageDoesNotMarkClosed(t *testing.T) {
	msgs := newFakeSlackMessages() // empty
	mappings := newFakeRepoMappings(store.RepoMapping{Repository: "octo/widget", SlackChannel: "C123"})
	h := newCloseHandler(t, msgs, mappings, &fakeSlackClient{}, true)

	e := pullrequest.Event{Action: "closed", Repository: "octo/widget", PR: pullrequest.PR{Number: 42}}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(msgs.closed) != 0 {
		t.Fatalf("MarkClosed recorded despite no stored message: %v", msgs.closed)
	}
}

func TestCloseHandler_Handle_NoStoredMessageIsNoop(t *testing.T) {
	msgs := newFakeSlackMessages() // empty
	mappings := newFakeRepoMappings(store.RepoMapping{Repository: "octo/widget", SlackChannel: "C123"})
	client := &fakeSlackClient{}
	h := newCloseHandler(t, msgs, mappings, client, true)

	e := pullrequest.Event{
		Action:     "closed",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 0 {
		t.Errorf("Slack called when no stored message: %v", client.methods())
	}
}
