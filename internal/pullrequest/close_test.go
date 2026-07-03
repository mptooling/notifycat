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
	return pullrequest.NewCloseHandler(st, behavior, client, testComposer(), discardLogger(), newFakeReviewSessions())
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
	h := pullrequest.NewCloseHandler(st, behavior, client, testComposer(), discardLogger(), newFakeReviewSessions())

	if err := h.Handle(context.Background(), closedMergedEvent("acme/web", 7)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if client.updates() != 2 || client.reactions() != 2 {
		t.Fatalf("want 2 updates + 2 reactions; got %d / %d", client.updates(), client.reactions())
	}
}

// ----- reviewed-by decoration -----

func TestCloseHandler_ReviewedByOnClose(t *testing.T) {
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := &fakeBehavior{m: storepkg.RepoMapping{Reactions: storepkg.Reactions{Enabled: false, MergedPR: "tada"}}}
	client := &fakeMessenger{}
	reviews := newFakeReviewSessions()
	key := prStoreKey("octo/widget", 42)
	reviews.reviewers[key] = []storepkg.CodeReview{
		{SlackUserID: "U1", SlackUserName: "Alice"},
		{SlackUserID: "U2", SlackUserName: "Bob"},
	}
	h := pullrequest.NewCloseHandler(st, behavior, client, testComposer(), discardLogger(), reviews)

	e := closedMergedEvent("octo/widget", 42)
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var foundReviewedBy bool
	for _, c := range client.calls {
		if c.Method != "UpdateMessage" {
			continue
		}
		for _, b := range c.Msg.Blocks {
			if b.Type == "context" && len(b.Elements) > 0 {
				if strings.Contains(b.Elements[0].Text, "reviewed by") &&
					strings.Contains(b.Elements[0].Text, "<@U1>") &&
					strings.Contains(b.Elements[0].Text, "<@U2>") {
					foundReviewedBy = true
				}
			}
		}
	}
	if !foundReviewedBy {
		t.Errorf("expected a 'reviewed by <@U1>, <@U2>' context block in UpdateMessage; calls=%+v", client.calls)
	}
}

func TestCloseHandler_NoReviewersNoReviewedByBlock(t *testing.T) {
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := &fakeBehavior{m: storepkg.RepoMapping{Reactions: storepkg.Reactions{Enabled: false}}}
	client := &fakeMessenger{}
	h := pullrequest.NewCloseHandler(st, behavior, client, testComposer(), discardLogger(), newFakeReviewSessions())

	e := closedMergedEvent("octo/widget", 42)
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	for _, c := range client.calls {
		if c.Method != "UpdateMessage" {
			continue
		}
		for _, b := range c.Msg.Blocks {
			if b.Type == "context" && len(b.Elements) > 0 && strings.Contains(b.Elements[0].Text, "reviewed by") {
				t.Errorf("unexpected 'reviewed by' block when no reviewers: %+v", b)
			}
		}
	}
	if client.updates() != 1 {
		t.Fatalf("close decoration should still happen; got %d updates", client.updates())
	}
}

func TestCloseHandler_ReviewedByDedup(t *testing.T) {
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := &fakeBehavior{m: storepkg.RepoMapping{Reactions: storepkg.Reactions{Enabled: false}}}
	client := &fakeMessenger{}
	reviews := newFakeReviewSessions()
	key := prStoreKey("octo/widget", 42)
	reviews.reviewers[key] = []storepkg.CodeReview{
		{SlackUserID: "U1", SlackUserName: "Alice"},
		{SlackUserID: "U1", SlackUserName: "Alice"},
		{SlackUserID: "U2", SlackUserName: "Bob"},
	}
	h := pullrequest.NewCloseHandler(st, behavior, client, testComposer(), discardLogger(), reviews)

	e := closedMergedEvent("octo/widget", 42)
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	for _, c := range client.calls {
		if c.Method != "UpdateMessage" {
			continue
		}
		for _, b := range c.Msg.Blocks {
			if b.Type == "context" && len(b.Elements) > 0 && strings.Contains(b.Elements[0].Text, "reviewed by") {
				text := b.Elements[0].Text
				// U1 should appear exactly once after dedup
				if strings.Count(text, "<@U1>") != 1 {
					t.Errorf("U1 should appear once after dedup; got %q", text)
				}
				if strings.Count(text, "<@U2>") != 1 {
					t.Errorf("U2 should appear once; got %q", text)
				}
			}
		}
	}
}

func TestCloseHandler_FinishesSessionOnClose(t *testing.T) {
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := &fakeBehavior{m: storepkg.RepoMapping{Reactions: storepkg.Reactions{Enabled: false}}}
	client := &fakeMessenger{}
	reviews := newFakeReviewSessions()
	h := pullrequest.NewCloseHandler(st, behavior, client, testComposer(), discardLogger(), reviews)

	e := closedMergedEvent("octo/widget", 42)
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.finishedCount("octo/widget", 42) != 1 {
		t.Fatalf("close should finish any active session; got finishedCount=%d", reviews.finishedCount("octo/widget", 42))
	}
	if st.closed[prStoreKey("octo/widget", 42)] != 1 {
		t.Fatalf("MarkClosed should still be called; got %d", st.closed[prStoreKey("octo/widget", 42)])
	}
}

func TestCloseHandler_ReviewersLoadFailureSoftDegrades(t *testing.T) {
	st := newFakePRStore()
	_ = st.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := &fakeBehavior{m: storepkg.RepoMapping{Reactions: storepkg.Reactions{Enabled: false}}}
	client := &fakeMessenger{}
	reviews := newFakeReviewSessions()
	reviews.listErr = errInjected
	h := pullrequest.NewCloseHandler(st, behavior, client, testComposer(), discardLogger(), reviews)

	e := closedMergedEvent("octo/widget", 42)
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle must not return error when reviewers load fails: %v", err)
	}
	if client.updates() != 1 {
		t.Fatalf("close decoration should still happen despite reviewer load error; got %d updates", client.updates())
	}
	for _, c := range client.calls {
		if c.Method != "UpdateMessage" {
			continue
		}
		for _, b := range c.Msg.Blocks {
			if b.Type == "context" && len(b.Elements) > 0 && strings.Contains(b.Elements[0].Text, "reviewed by") {
				t.Errorf("'reviewed by' block should be absent on load error; got %+v", b)
			}
		}
	}
	if st.closed[prStoreKey("octo/widget", 42)] != 1 {
		t.Fatalf("MarkClosed should still be called; got %d", st.closed[prStoreKey("octo/widget", 42)])
	}
}
