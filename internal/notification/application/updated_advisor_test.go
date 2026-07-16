package application_test

import (
	"context"
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	saliencedomain "github.com/mptooling/notifycat/internal/salience/domain"
)

func lifecycleParams(store *fakeMessageStore, messenger *fakeMessenger, advisor *fakeAdvisor, mapping routingdomain.RepoMapping) domain.LifecycleHandlerParams {
	return domain.LifecycleHandlerParams{
		Store:     store,
		Behavior:  &fakeBehavior{mapping: mapping},
		Messenger: messenger,
		Advisor:   advisor,
		Logger:    discardLogger(),
		Reviews:   &fakeReviewSessions{activeErr: domain.ErrNoActiveReview},
	}
}

func advisorTestBehavior() routingdomain.RepoMapping {
	return routingdomain.RepoMapping{
		Repository:   "acme/api",
		SlackChannel: "C1",
		Reactions:    routingdomain.Reactions{Enabled: true, NewPR: "eyes", MergedPR: "twisted_rightwards_arrows", ClosedPR: "x", Approved: "white_check_mark"},
	}
}

func TestApproveHandlerUsesDecidedEmoji(t *testing.T) {
	store, messenger, advisor := newFakeMessageStore(), &fakeMessenger{}, newFakeAdvisor()
	store.seed("acme/api", 7, domain.Message{Channel: "C1", MessageID: "ts-1"})
	advisor.updatedDecision = &saliencedomain.UpdatedDecision{Emoji: "rocket"}
	handler := application.NewApproveHandler(lifecycleParams(store, messenger, advisor, advisorTestBehavior()))

	event := kernel.Event{Kind: kernel.KindApproved, Repository: "acme/api", PR: kernel.PR{Number: 7}, Sender: kernel.Sender{Login: "bob"}}
	if err := handler.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	if got := messenger.reactionEmojis(); len(got) != 1 || got[0] != "rocket" {
		t.Errorf("reactions = %v; want the decided emoji", got)
	}
	if len(advisor.updatedRequests) != 1 || advisor.updatedRequests[0].DefaultEmoji != "white_check_mark" || advisor.updatedRequests[0].Kind != "approved" {
		t.Errorf("advisor request = %+v", advisor.updatedRequests)
	}
}

func TestApproveHandlerBotSuppressionSkipsAdvisor(t *testing.T) {
	store, messenger, advisor := newFakeMessageStore(), &fakeMessenger{}, newFakeAdvisor()
	store.seed("acme/api", 7, domain.Message{Channel: "C1", MessageID: "ts-1"})
	behavior := advisorTestBehavior()
	behavior.IgnoreAIReviews = true
	handler := application.NewApproveHandler(lifecycleParams(store, messenger, advisor, behavior))

	event := kernel.Event{Kind: kernel.KindApproved, Repository: "acme/api", PR: kernel.PR{Number: 7}, Sender: kernel.Sender{Login: "copilot", IsBot: true}}
	if err := handler.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	if len(advisor.updatedRequests) != 0 {
		t.Error("advisor consulted for a policy-suppressed bot review; policy outranks AI")
	}
	if len(messenger.reactions) != 0 {
		t.Errorf("reactions = %v; want none", messenger.reactionEmojis())
	}
}

func TestCloseHandlerUsesDecidedEmoji(t *testing.T) {
	store, messenger, advisor := newFakeMessageStore(), &fakeMessenger{}, newFakeAdvisor()
	store.seed("acme/api", 7, domain.Message{Channel: "C1", MessageID: "ts-1"})
	advisor.updatedDecision = &saliencedomain.UpdatedDecision{Emoji: "sparkles"}
	handler := application.NewCloseHandler(lifecycleParams(store, messenger, advisor, advisorTestBehavior()))

	event := kernel.Event{Kind: kernel.KindMerged, Repository: "acme/api", PR: kernel.PR{Number: 7, Merged: true}, Sender: kernel.Sender{Login: "alice"}}
	if err := handler.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	if len(messenger.closes) != 1 || messenger.closes[0].req.Emoji != "sparkles" {
		t.Errorf("UpdateClosed emoji = %+v; want the decided emoji", messenger.closes)
	}
	if got := messenger.reactionEmojis(); len(got) != 1 || got[0] != "sparkles" {
		t.Errorf("reactions = %v; want the decided emoji", got)
	}
	if len(advisor.updatedRequests) != 1 || advisor.updatedRequests[0].DefaultEmoji != "twisted_rightwards_arrows" {
		t.Errorf("advisor request default = %+v; want the merged emoji", advisor.updatedRequests)
	}
}
