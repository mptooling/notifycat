package application_test

import (
	"context"
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

func newCloseHandler(store *fakeMessageStore, behavior *fakeBehavior, messenger *fakeMessenger) *application.CloseHandler {
	return application.NewCloseHandler(store, behavior, messenger, discardLogger(), &fakeReviewSessions{})
}

// closedMergedEvent returns a merged event with PR.Merged = true.
func closedMergedEvent(repo string, prNumber int) kernel.Event {
	return kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindMerged,
		Repository: repo,
		PR:         kernel.PR{Number: prNumber, Title: "fix", URL: "u", Author: "a", Merged: true},
	}
}

func TestCloseHandler_Applicable(t *testing.T) {
	h := newCloseHandler(newFakeMessageStore(), &fakeBehavior{}, &fakeMessenger{})

	if !h.Applicable(kernel.Event{Kind: kernel.KindClosed}) {
		t.Error("closed should be applicable")
	}
	if h.Applicable(kernel.Event{Kind: kernel.KindOpened}) {
		t.Error("opened should not be applicable")
	}
}

func TestCloseHandler_Handle_UpdatesMessage(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})

	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{
		Reactions: routingdomain.Reactions{
			Enabled:  true,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
	messenger := &fakeMessenger{}
	h := newCloseHandler(store, behavior, messenger)

	e := kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindMerged,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "fix", URL: "u", Author: "a", Merged: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.closes) != 1 {
		t.Errorf("UpdateClosed not called; closes = %d", len(messenger.closes))
	}
	if len(messenger.reactions) != 1 {
		t.Errorf("AddReaction not called when reactions enabled; reactions = %d", len(messenger.reactions))
	}
	for _, r := range messenger.reactions {
		if r.emoji != "twisted_rightwards_arrows" {
			t.Errorf("AddReaction emoji = %q; want twisted_rightwards_arrows", r.emoji)
		}
	}
	for _, c := range messenger.closes {
		if !c.req.Merged {
			t.Errorf("ClosedRequest.Merged should be true for merged PR")
		}
		if c.req.Emoji != "twisted_rightwards_arrows" {
			t.Errorf("ClosedRequest.Emoji = %q; want twisted_rightwards_arrows", c.req.Emoji)
		}
	}
}

func TestCloseHandler_Handle_ClosedNotMergedUsesClosedEmoji(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})

	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{
		Reactions: routingdomain.Reactions{
			Enabled:  true,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
	messenger := &fakeMessenger{}
	h := newCloseHandler(store, behavior, messenger)

	e := kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindClosed,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Merged: false},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	for _, r := range messenger.reactions {
		if r.emoji != "x" {
			t.Errorf("AddReaction emoji = %q; want x", r.emoji)
		}
	}
	for _, c := range messenger.closes {
		if c.req.Emoji != "x" {
			t.Errorf("ClosedRequest.Emoji = %q; want x for closed-not-merged", c.req.Emoji)
		}
	}
}

func TestCloseHandler_Handle_NoReactionWhenDisabled(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})

	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{
		Reactions: routingdomain.Reactions{
			Enabled:  false,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
	messenger := &fakeMessenger{}
	h := newCloseHandler(store, behavior, messenger)

	e := kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindMerged,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Merged: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.reactions) != 0 {
		t.Errorf("AddReaction called when reactions disabled: %v", messenger.reactionEmojis())
	}
}

func TestCloseHandler_Handle_MarksClosed(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})

	// Reactions disabled to prove MarkClosed is independent of reactions.
	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{
		Reactions: routingdomain.Reactions{
			Enabled:  false,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
	messenger := &fakeMessenger{}
	h := newCloseHandler(store, behavior, messenger)

	e := kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindMerged,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Merged: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !store.closed[storeKey("octo/widget", 42)] {
		t.Fatalf("MarkClosed not recorded for the PR: closed=%v", store.closed)
	}
}

func TestCloseHandler_Handle_NoStoredMessageDoesNotMarkClosed(t *testing.T) {
	store := newFakeMessageStore() // empty

	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{
		Reactions: routingdomain.Reactions{
			Enabled:  true,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
	h := newCloseHandler(store, behavior, &fakeMessenger{})

	e := kernel.Event{Provider: kernel.ProviderGitHub, Kind: kernel.KindClosed, Repository: "octo/widget", PR: kernel.PR{Number: 42}}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if store.closed[storeKey("octo/widget", 42)] {
		t.Fatalf("MarkClosed recorded despite no stored message: closed=%v", store.closed)
	}
}

func TestCloseHandler_Handle_NoStoredMessageIsNoop(t *testing.T) {
	store := newFakeMessageStore() // empty

	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{
		Reactions: routingdomain.Reactions{
			Enabled:  true,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
	messenger := &fakeMessenger{}
	h := newCloseHandler(store, behavior, messenger)

	e := kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindClosed,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.closes) != 0 || len(messenger.reactions) != 0 {
		t.Errorf("messenger called when no stored message: closes=%d reactions=%d", len(messenger.closes), len(messenger.reactions))
	}
}

func TestCloseHandler_ActsOnEveryMessage(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("acme/web", 7, domain.Message{Channel: "C0A", MessageID: "100.1"})
	store.seed("acme/web", 7, domain.Message{Channel: "C0B", MessageID: "200.1"})
	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{Reactions: routingdomain.Reactions{Enabled: true, MergedPR: "tada"}}}
	messenger := &fakeMessenger{}
	h := application.NewCloseHandler(store, behavior, messenger, discardLogger(), &fakeReviewSessions{})

	if err := h.Handle(context.Background(), closedMergedEvent("acme/web", 7)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(messenger.closes) != 2 || len(messenger.reactions) != 2 {
		t.Fatalf("want 2 updates + 2 reactions; got %d / %d", len(messenger.closes), len(messenger.reactions))
	}
}

// ----- reviewed-by decoration -----

func TestCloseHandler_ReviewedByOnClose(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{Reactions: routingdomain.Reactions{Enabled: false, MergedPR: "tada"}}}
	messenger := &fakeMessenger{}
	reviews := &fakeReviewSessions{
		reviewers: []domain.ReviewSession{
			{SlackUserID: "U1", SlackUserName: "Alice"},
			{SlackUserID: "U2", SlackUserName: "Bob"},
		},
	}
	h := application.NewCloseHandler(store, behavior, messenger, discardLogger(), reviews)

	e := closedMergedEvent("octo/widget", 42)
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.closes) != 1 {
		t.Fatalf("want 1 UpdateClosed call; got %d", len(messenger.closes))
	}
	req := messenger.closes[0].req
	if len(req.ReviewerIDs) != 2 {
		t.Errorf("ReviewerIDs should have 2 entries; got %v", req.ReviewerIDs)
	}
	foundU1, foundU2 := false, false
	for _, id := range req.ReviewerIDs {
		if id == "U1" {
			foundU1 = true
		}
		if id == "U2" {
			foundU2 = true
		}
	}
	if !foundU1 || !foundU2 {
		t.Errorf("ReviewerIDs should contain U1 and U2; got %v", req.ReviewerIDs)
	}
}

func TestCloseHandler_NoReviewersNoReviewedByBlock(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{Reactions: routingdomain.Reactions{Enabled: false}}}
	messenger := &fakeMessenger{}
	h := application.NewCloseHandler(store, behavior, messenger, discardLogger(), &fakeReviewSessions{})

	e := closedMergedEvent("octo/widget", 42)
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.closes) != 1 {
		t.Fatalf("close decoration should still happen; got %d closes", len(messenger.closes))
	}
	req := messenger.closes[0].req
	if len(req.ReviewerIDs) != 0 {
		t.Errorf("ReviewerIDs should be empty when no reviewers; got %v", req.ReviewerIDs)
	}
}

func TestCloseHandler_ReviewedByDedup(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{Reactions: routingdomain.Reactions{Enabled: false}}}
	messenger := &fakeMessenger{}
	reviews := &fakeReviewSessions{
		reviewers: []domain.ReviewSession{
			{SlackUserID: "U1", SlackUserName: "Alice"},
			{SlackUserID: "U1", SlackUserName: "Alice"},
			{SlackUserID: "U2", SlackUserName: "Bob"},
		},
	}
	h := application.NewCloseHandler(store, behavior, messenger, discardLogger(), reviews)

	e := closedMergedEvent("octo/widget", 42)
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.closes) != 1 {
		t.Fatalf("want 1 UpdateClosed call; got %d", len(messenger.closes))
	}
	req := messenger.closes[0].req
	countU1 := 0
	countU2 := 0
	for _, id := range req.ReviewerIDs {
		if id == "U1" {
			countU1++
		}
		if id == "U2" {
			countU2++
		}
	}
	if countU1 != 1 {
		t.Errorf("U1 should appear exactly once after dedup; got %d in %v", countU1, req.ReviewerIDs)
	}
	if countU2 != 1 {
		t.Errorf("U2 should appear exactly once; got %d in %v", countU2, req.ReviewerIDs)
	}
}

func TestCloseHandler_FinishesSessionOnClose(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{Reactions: routingdomain.Reactions{Enabled: false}}}
	messenger := &fakeMessenger{}
	reviews := &fakeReviewSessions{}
	h := application.NewCloseHandler(store, behavior, messenger, discardLogger(), reviews)

	e := closedMergedEvent("octo/widget", 42)
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.finished != 1 {
		t.Fatalf("close should finish any active session; got finished=%d", reviews.finished)
	}
	if !store.closed[storeKey("octo/widget", 42)] {
		t.Fatalf("MarkClosed should still be called; got closed=%v", store.closed)
	}
}

func TestCloseHandler_ReviewersLoadFailureSoftDegrades(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{Reactions: routingdomain.Reactions{Enabled: false}}}
	messenger := &fakeMessenger{}
	reviews := &fakeReviewSessions{reviewersErr: errInjected}
	h := application.NewCloseHandler(store, behavior, messenger, discardLogger(), reviews)

	e := closedMergedEvent("octo/widget", 42)
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle must not return error when reviewers load fails: %v", err)
	}
	if len(messenger.closes) != 1 {
		t.Fatalf("close decoration should still happen despite reviewer load error; got %d closes", len(messenger.closes))
	}
	req := messenger.closes[0].req
	if len(req.ReviewerIDs) != 0 {
		t.Errorf("ReviewerIDs should be empty on load error; got %v", req.ReviewerIDs)
	}
	if !store.closed[storeKey("octo/widget", 42)] {
		t.Fatalf("MarkClosed should still be called; got closed=%v", store.closed)
	}
}
