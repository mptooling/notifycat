package application_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	saliencedomain "github.com/mptooling/notifycat/internal/salience/domain"
)

func openedEvent() kernel.Event {
	return kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindOpened,
		Repository: "acme/api",
		PR:         kernel.PR{Number: 7, Title: "add rate limiter", URL: "https://github.com/acme/api/pull/7", Author: "alice", Body: "body"},
		Sender:     kernel.Sender{Login: "alice"},
	}
}

func openHandlerUnderTest(store *fakeMessageStore, messenger *fakeMessenger, advisor *fakeAdvisor, resolver *fakeTargetResolver) *application.OpenHandler {
	return application.NewOpenHandler(domain.OpenHandlerParams{
		Store:     store,
		Resolver:  resolver,
		Messenger: messenger,
		Advisor:   advisor,
		Logger:    discardLogger(),
	})
}

func standardResolver() *fakeTargetResolver {
	return &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{
			Repository:     "acme/api",
			SlackChannel:   "C1",
			Mentions:       []string{"<@U1>"},
			Reactions:      routingdomain.Reactions{Enabled: true, NewPR: "eyes"},
			AIEnabled:      true,
			AIInstructions: "be concise",
		},
		targets:      []routingdomain.Target{{Channel: "C1", Mentions: []string{"<@U1>"}}},
		changedFiles: []string{"services/payments/main.go"},
	}
}

func TestOpenHandlerBuildsAdvisorRequest(t *testing.T) {
	store, messenger, advisor := newFakeMessageStore(), &fakeMessenger{}, newFakeAdvisor()
	handler := openHandlerUnderTest(store, messenger, advisor, standardResolver())

	if err := handler.Handle(context.Background(), openedEvent()); err != nil {
		t.Fatal(err)
	}

	if len(advisor.openRequests) != 1 {
		t.Fatalf("advisor consulted %d times; want 1", len(advisor.openRequests))
	}
	request := advisor.openRequests[0]
	if request.Repository != "acme/api" || request.PR.Number != 7 || request.PR.Title != "add rate limiter" {
		t.Errorf("request PR fields wrong: %+v", request)
	}
	if !reflect.DeepEqual(request.ChangedFiles, []string{"services/payments/main.go"}) {
		t.Errorf("ChangedFiles = %v", request.ChangedFiles)
	}
	if !reflect.DeepEqual(request.Candidates, []saliencedomain.CandidateTarget{{Channel: "C1", Mentions: []string{"<@U1>"}}}) {
		t.Errorf("Candidates = %+v", request.Candidates)
	}
	if request.DefaultEmoji != "eyes" {
		t.Errorf("DefaultEmoji = %q", request.DefaultEmoji)
	}
	// Per-tier opt-out fields: a resolver that supplies TierEnabled=false must
	// propagate to the request so the advisor short-circuits for that tier.
	if !request.TierEnabled {
		t.Errorf("TierEnabled = false; want true from standardResolver")
	}
	if request.Instructions != "be concise" {
		t.Errorf("Instructions = %q; want %q", request.Instructions, "be concise")
	}
	// EmojiAllowlist is the resolved reactions plus curated extras; at minimum
	// the configured NewPR emoji and the curated set must be present.
	emojiSet := make(map[string]bool, len(request.EmojiAllowlist))
	for _, e := range request.EmojiAllowlist {
		emojiSet[e] = true
	}
	if !emojiSet["eyes"] {
		t.Errorf("EmojiAllowlist missing the configured NewPR emoji %q: %v", "eyes", request.EmojiAllowlist)
	}
	for _, curated := range saliencedomain.CuratedEmojis {
		if !emojiSet[curated] {
			t.Errorf("EmojiAllowlist missing curated emoji %q: %v", curated, request.EmojiAllowlist)
		}
	}
}

func TestOpenHandlerQuietDecisionDropsMentions(t *testing.T) {
	store, messenger, advisor := newFakeMessageStore(), &fakeMessenger{}, newFakeAdvisor()
	advisor.openDecision = &saliencedomain.OpenDecision{Targets: []saliencedomain.TargetDecision{{
		Channel: "C1", Loudness: saliencedomain.LoudnessQuiet, Mentions: []string{"<@U1>"},
		LeadingEmoji: "package", Format: saliencedomain.FormatCompact, Emphasis: saliencedomain.EmphasisNone,
		ContextBlock: "docs only",
	}}}
	handler := openHandlerUnderTest(store, messenger, advisor, standardResolver())

	if err := handler.Handle(context.Background(), openedEvent()); err != nil {
		t.Fatal(err)
	}

	if len(messenger.opens) != 1 {
		t.Fatalf("opens = %d; want 1 — quiet still posts", len(messenger.opens))
	}
	posted := messenger.opens[0].req
	if posted.Mentions != nil {
		t.Errorf("Mentions = %v; quiet must drop them", posted.Mentions)
	}
	if !posted.Compact || posted.NewPREmoji != "package" || posted.ContextBlock != "docs only" {
		t.Errorf("decision fields not applied: %+v", posted)
	}
}

func TestOpenHandlerBotCompactPolicySkipsAdvisor(t *testing.T) {
	store, messenger, advisor := newFakeMessageStore(), &fakeMessenger{}, newFakeAdvisor()
	resolver := standardResolver()
	resolver.behavior.DependabotFormat = true
	event := openedEvent()
	event.PR.Author = "dependabot[bot]"
	handler := openHandlerUnderTest(store, messenger, advisor, resolver)

	if err := handler.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	if len(advisor.openRequests) != 0 {
		t.Errorf("advisor consulted for a rule-sufficient bot PR; policy outranks AI")
	}
	if len(messenger.opens) != 1 || messenger.opens[0].req.Bot == nil {
		t.Errorf("bot compact post missing: %+v", messenger.opens)
	}
}
