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
			Repository:   "acme/api",
			SlackChannel: "C1",
			Mentions:     []string{"<@U1>"},
			Reactions:    routingdomain.Reactions{Enabled: true, NewPR: "eyes"},
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

func TestOpenHandlerPostsThreadNote(t *testing.T) {
	store, messenger, advisor := newFakeMessageStore(), &fakeMessenger{}, newFakeAdvisor()
	advisor.openDecision = &saliencedomain.OpenDecision{Targets: []saliencedomain.TargetDecision{{
		Channel: "C1", Loudness: saliencedomain.LoudnessPing, LeadingEmoji: "eyes",
		Format: saliencedomain.FormatStandard, Emphasis: saliencedomain.EmphasisNone,
		ThreadNote: "third PR touching payments this week",
	}}}
	handler := openHandlerUnderTest(store, messenger, advisor, standardResolver())

	if err := handler.Handle(context.Background(), openedEvent()); err != nil {
		t.Fatal(err)
	}

	if len(messenger.threadNotes) != 1 {
		t.Fatalf("threadNotes = %d; want 1", len(messenger.threadNotes))
	}
	note := messenger.threadNotes[0]
	if note.channel != "C1" || note.messageID != "ts-1" || note.req.Note != "third PR touching payments this week" {
		t.Errorf("thread note = %+v", note)
	}
}

func TestOpenHandlerThreadNoteFailureIsSoft(t *testing.T) {
	store, messenger, advisor := newFakeMessageStore(), &fakeMessenger{}, newFakeAdvisor()
	messenger.threadNoteErr = context.DeadlineExceeded
	advisor.openDecision = &saliencedomain.OpenDecision{Targets: []saliencedomain.TargetDecision{{
		Channel: "C1", Loudness: saliencedomain.LoudnessPing, LeadingEmoji: "eyes",
		Format: saliencedomain.FormatStandard, Emphasis: saliencedomain.EmphasisNone,
		ThreadNote: "note",
	}}}
	handler := openHandlerUnderTest(store, messenger, advisor, standardResolver())

	if err := handler.Handle(context.Background(), openedEvent()); err != nil {
		t.Fatalf("a failed thread note must not fail the delivery; got %v", err)
	}
	if len(messenger.opens) != 1 {
		t.Errorf("message must still post; opens = %d", len(messenger.opens))
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
