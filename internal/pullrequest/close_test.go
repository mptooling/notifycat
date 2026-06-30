package pullrequest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/pullrequest"
	storepkg "github.com/mptooling/notifycat/internal/store"
)

func newCloseHandler(t *testing.T, st *fakePRStore, behavior *fakeBehavior, client *fakeMessenger) *pullrequest.CloseHandler {
	t.Helper()
	return pullrequest.NewCloseHandler(st, behavior, client, testComposer(), discardLogger())
}

// closedMergedEvent returns a "closed" event with PR.Merged = true.
func closedMergedEvent(repo string, prNumber int) pullrequest.Event {
	return pullrequest.Event{
		Action:     "closed",
		Repository: repo,
		PR:         pullrequest.PR{Number: prNumber, Title: "fix", URL: "u", Author: "a", Merged: true},
	}
}

func TestCloseHandler_Applicable(t *testing.T) {
	h := newCloseHandler(t, newFakePRStore(), &fakeBehavior{}, &fakeMessenger{})

	if !h.Applicable(pullrequest.Event{Action: "closed"}) {
		t.Error("closed should be applicable")
	}
	if h.Applicable(pullrequest.Event{Action: "opened"}) {
		t.Error("opened should not be applicable")
	}
}

func TestCloseHandler_Handle_UpdatesMessage(t *testing.T) {
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")

	behavior := &fakeBehavior{m: storepkg.RepoMapping{
		Reactions: storepkg.Reactions{
			Enabled:  true,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
	client := &fakeMessenger{}
	h := newCloseHandler(t, st, behavior, client)

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
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")

	behavior := &fakeBehavior{m: storepkg.RepoMapping{
		Reactions: storepkg.Reactions{
			Enabled:  true,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
	client := &fakeMessenger{}
	h := newCloseHandler(t, st, behavior, client)

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
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")

	behavior := &fakeBehavior{m: storepkg.RepoMapping{
		Reactions: storepkg.Reactions{
			Enabled:  false,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
	client := &fakeMessenger{}
	h := newCloseHandler(t, st, behavior, client)

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
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")

	// Reactions disabled to prove MarkClosed is independent of reactions.
	behavior := &fakeBehavior{m: storepkg.RepoMapping{
		Reactions: storepkg.Reactions{
			Enabled:  false,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
	client := &fakeMessenger{}
	h := newCloseHandler(t, st, behavior, client)

	e := pullrequest.Event{
		Action:     "closed",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42, Merged: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if st.closed[prStoreKey("octo/widget", 42)] != 1 {
		t.Fatalf("MarkClosed not recorded for the PR: closed=%v", st.closed)
	}
}

func TestCloseHandler_Handle_NoStoredMessageDoesNotMarkClosed(t *testing.T) {
	st := newFakePRStore() // empty

	behavior := &fakeBehavior{m: storepkg.RepoMapping{
		Reactions: storepkg.Reactions{
			Enabled:  true,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
	h := newCloseHandler(t, st, behavior, &fakeMessenger{})

	e := pullrequest.Event{Action: "closed", Repository: "octo/widget", PR: pullrequest.PR{Number: 42}}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if st.closed[prStoreKey("octo/widget", 42)] != 0 {
		t.Fatalf("MarkClosed recorded despite no stored message: closed=%v", st.closed)
	}
}

func TestCloseHandler_Handle_NoStoredMessageIsNoop(t *testing.T) {
	st := newFakePRStore() // empty

	behavior := &fakeBehavior{m: storepkg.RepoMapping{
		Reactions: storepkg.Reactions{
			Enabled:  true,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
	client := &fakeMessenger{}
	h := newCloseHandler(t, st, behavior, client)

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

func TestCloseHandler_ActsOnEveryMessage(t *testing.T) {
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "acme/web", 7, "C0A", "100.1")
	_ = st.AddMessage(context.Background(), "acme/web", 7, "C0B", "200.1")
	behavior := &fakeBehavior{m: storepkg.RepoMapping{Reactions: storepkg.Reactions{Enabled: true, MergedPR: "tada"}}}
	client := &fakeMessenger{}
	h := pullrequest.NewCloseHandler(st, behavior, client, testComposer(), discardLogger())

	if err := h.Handle(context.Background(), closedMergedEvent("acme/web", 7)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if client.updates() != 2 || client.reactions() != 2 {
		t.Fatalf("want 2 updates + 2 reactions; got %d / %d", client.updates(), client.reactions())
	}
}
