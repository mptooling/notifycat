package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

func newOpenHandler(
	store *fakeMessageStore,
	resolver *fakeTargetResolver,
	messenger *fakeMessenger,
) *application.OpenHandler {
	return application.NewOpenHandler(store, resolver, messenger, discardLogger())
}

func openedEvent(repo string, prNumber int) kernel.Event {
	return kernel.Event{
		Action:     kernel.ActionOpened,
		Repository: repo,
		PR:         kernel.PR{Number: prNumber, Title: fmt.Sprintf("PR #%d", prNumber), Draft: false},
	}
}

func TestOpenHandler_Applicable(t *testing.T) {
	h := newOpenHandler(newFakeMessageStore(), &fakeTargetResolver{}, &fakeMessenger{})

	cases := []struct {
		name string
		e    kernel.Event
		want bool
	}{
		{"opened non-draft", kernel.Event{Action: kernel.ActionOpened, PR: kernel.PR{Draft: false}}, true},
		{"opened draft", kernel.Event{Action: kernel.ActionOpened, PR: kernel.PR{Draft: true}}, false},
		{"ready_for_review", kernel.Event{Action: kernel.ActionReadyForReview}, true},
		{"closed", kernel.Event{Action: kernel.ActionClosed}, false},
		{"submitted approved", kernel.Event{Action: kernel.ActionSubmitted, Review: &kernel.Review{State: kernel.ReviewApproved}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := h.Applicable(c.e); got != c.want {
				t.Errorf("Applicable(%+v) = %v; want %v", c.e, got, c.want)
			}
		})
	}
}

func TestOpenHandler_Handle_PostsAndStoresTS(t *testing.T) {
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{
			DependabotFormat: true,
			Reactions:        routingdomain.Reactions{NewPR: "rocket"},
		},
		targets: []routingdomain.Target{
			{Channel: "C123", Mentions: []string{"@alice"}},
		},
	}
	messenger := &fakeMessenger{}
	h := newOpenHandler(store, resolver, messenger)

	e := kernel.Event{
		Action:     kernel.ActionOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "fix", URL: "u", Author: "a", Draft: false},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.opens) != 1 {
		t.Fatalf("want 1 PostOpen call; got %d", len(messenger.opens))
	}
	if messenger.opens[0].channel != "C123" {
		t.Errorf("channel = %q; want C123", messenger.opens[0].channel)
	}

	msgs, err := store.Messages(context.Background(), "octo/widget", 42)
	if err != nil {
		t.Fatalf("Messages after Handle: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Channel != "C123" || msgs[0].MessageID == "" {
		t.Errorf("unexpected stored messages: %+v", msgs)
	}
}

func TestOpenHandler_Handle_ThreadsCreatedAtAndFallback(t *testing.T) {
	// This test verified that the context line and fallback are populated; those
	// are now messenger-layer concerns. We assert the domain intent: the PR fields
	// are forwarded correctly into the OpenRequest so the messenger can render them.
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{
			DependabotFormat: true,
			Reactions:        routingdomain.Reactions{NewPR: "rocket"},
		},
		targets: []routingdomain.Target{
			{Channel: "C123", Mentions: []string{"@alice"}},
		},
	}
	messenger := &fakeMessenger{}
	h := newOpenHandler(store, resolver, messenger)

	e := kernel.Event{
		Action:     kernel.ActionOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "fix", URL: "u", Author: "alice"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.opens) != 1 {
		t.Fatalf("want 1 PostOpen call; got %d", len(messenger.opens))
	}
	req := messenger.opens[0].req
	if req.PR.Author != "alice" || req.PR.Number != 42 || req.Repository != "octo/widget" {
		t.Errorf("OpenRequest fields not forwarded correctly: %+v", req)
	}
}

func TestOpenHandler_Handle_SkipsIfMessageAlreadyExists(t *testing.T) {
	store := newFakeMessageStore()
	// Pre-seed a message for channel C123 so the handler should skip it.
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "preexisting-ts"})

	resolver := &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{
			DependabotFormat: true,
			Reactions:        routingdomain.Reactions{NewPR: "rocket"},
		},
		targets: []routingdomain.Target{
			{Channel: "C123"},
		},
	}
	messenger := &fakeMessenger{}
	h := newOpenHandler(store, resolver, messenger)

	e := kernel.Event{
		Action:     kernel.ActionOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.opens) != 0 {
		t.Errorf("PostOpen called when message already existed: %d calls", len(messenger.opens))
	}
}

func TestOpenHandler_Handle_SkipsIfNoMapping(t *testing.T) {
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{err: routingdomain.ErrNotFound}
	messenger := &fakeMessenger{}
	h := newOpenHandler(store, resolver, messenger)

	e := kernel.Event{
		Action:     kernel.ActionOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.opens) != 0 {
		t.Errorf("PostOpen called when no mapping existed: %d calls", len(messenger.opens))
	}
}

func TestOpenHandler_Handle_DependabotRoutine(t *testing.T) {
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{
			DependabotFormat: true,
			Reactions:        routingdomain.Reactions{NewPR: "rocket"},
		},
		targets: []routingdomain.Target{
			{Channel: "C123", Mentions: []string{"@alice"}},
		},
	}
	messenger := &fakeMessenger{}
	h := newOpenHandler(store, resolver, messenger)

	e := kernel.Event{
		Action:     kernel.ActionOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "bump acme/lib from 1.2.0 to 1.2.1", URL: "u", Author: "dependabot[bot]"},
		Sender:     kernel.Sender{Login: "dependabot[bot]", Type: kernel.SenderTypeBot},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.opens) != 1 {
		t.Fatalf("want 1 PostOpen call; got %d", len(messenger.opens))
	}
	req := messenger.opens[0].req
	if req.Bot == nil {
		t.Fatal("OpenRequest.Bot should be non-nil for dependabot routine PR")
	}
	if req.Bot.Name != "dependabot" {
		t.Errorf("Bot.Name = %q; want dependabot", req.Bot.Name)
	}
	if req.Bot.Security {
		t.Error("Bot.Security should be false for routine PR")
	}
}

func TestOpenHandler_Handle_DependabotSecurity(t *testing.T) {
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{
			DependabotFormat: true,
			Reactions:        routingdomain.Reactions{NewPR: "rocket"},
		},
		targets: []routingdomain.Target{
			{Channel: "C123"},
		},
	}
	messenger := &fakeMessenger{}
	h := newOpenHandler(store, resolver, messenger)

	e := kernel.Event{
		Action:     kernel.ActionOpened,
		Repository: "octo/widget",
		PR: kernel.PR{
			Number: 42, Title: "bump acme/lib from 1.2.0 to 1.2.1", URL: "u", Author: "dependabot[bot]",
			Body: "Bumps acme/lib.\n\n## Vulnerabilities fixed\n\nCVE-2026-1234.",
		},
		Sender: kernel.Sender{Login: "dependabot[bot]", Type: kernel.SenderTypeBot},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.opens) != 1 {
		t.Fatalf("want 1 PostOpen call; got %d", len(messenger.opens))
	}
	req := messenger.opens[0].req
	if req.Bot == nil {
		t.Fatal("OpenRequest.Bot should be non-nil for dependabot security PR")
	}
	if req.Bot.Name != "dependabot" {
		t.Errorf("Bot.Name = %q; want dependabot", req.Bot.Name)
	}
	if !req.Bot.Security {
		t.Error("Bot.Security should be true for security advisory PR")
	}
}

func TestOpenHandler_Handle_Renovate(t *testing.T) {
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{
			DependabotFormat: true,
			Reactions:        routingdomain.Reactions{NewPR: "rocket"},
		},
		targets: []routingdomain.Target{
			{Channel: "C123"},
		},
	}
	messenger := &fakeMessenger{}
	h := newOpenHandler(store, resolver, messenger)

	e := kernel.Event{
		Action:     kernel.ActionOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 7, Title: "Update acme/lib to v2", URL: "u", Author: "renovate[bot]"},
		Sender:     kernel.Sender{Login: "renovate[bot]", Type: kernel.SenderTypeBot},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.opens) != 1 {
		t.Fatalf("want 1 PostOpen call; got %d", len(messenger.opens))
	}
	req := messenger.opens[0].req
	if req.Bot == nil {
		t.Fatal("OpenRequest.Bot should be non-nil for renovate PR")
	}
	if req.Bot.Name != "renovate" {
		t.Errorf("Bot.Name = %q; want renovate", req.Bot.Name)
	}
}

func TestOpenHandler_Handle_DependabotReadyForReviewByHuman(t *testing.T) {
	// Regression: a draft Dependabot PR marked ready_for_review by a human. The
	// webhook sender is the human who clicked the button, but the PR author is
	// still dependabot[bot] — detection must key off the author so the compact
	// format applies, not off the sender (which would fall back to "please
	// review").
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{
			DependabotFormat: true,
			Reactions:        routingdomain.Reactions{NewPR: "rocket"},
		},
		targets: []routingdomain.Target{
			{Channel: "C123"},
		},
	}
	messenger := &fakeMessenger{}
	h := newOpenHandler(store, resolver, messenger)

	e := kernel.Event{
		Action:     kernel.ActionReadyForReview,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "bump acme/lib from 1.2.0 to 1.2.1", URL: "u", Author: "dependabot[bot]"},
		Sender:     kernel.Sender{Login: "alice", Type: kernel.SenderTypeUser},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.opens) != 1 {
		t.Fatalf("want 1 PostOpen call; got %d", len(messenger.opens))
	}
	req := messenger.opens[0].req
	if req.Bot == nil {
		t.Fatal("OpenRequest.Bot should be non-nil: detection must key off PR author, not sender")
	}
	if req.Bot.Name != "dependabot" {
		t.Errorf("Bot.Name = %q; want dependabot", req.Bot.Name)
	}
}

func TestOpenHandler_Handle_DependabotFormatDisabled(t *testing.T) {
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{
			DependabotFormat: false,
		},
		targets: []routingdomain.Target{
			{Channel: "C123"},
		},
	}
	messenger := &fakeMessenger{}
	h := newOpenHandler(store, resolver, messenger)

	e := kernel.Event{
		Action:     kernel.ActionOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "bump acme/lib", URL: "u", Author: "dependabot[bot]"},
		Sender:     kernel.Sender{Login: "dependabot[bot]", Type: kernel.SenderTypeBot},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.opens) != 1 {
		t.Fatalf("want 1 PostOpen call; got %d", len(messenger.opens))
	}
	req := messenger.opens[0].req
	if req.Bot != nil {
		t.Errorf("with format disabled, OpenRequest.Bot should be nil; got %+v", req.Bot)
	}
}

func TestOpenHandler_Handle_DependabotEmptyMentions(t *testing.T) {
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{
			DependabotFormat: true,
			Reactions:        routingdomain.Reactions{NewPR: "rocket"},
		},
		targets: []routingdomain.Target{
			{Channel: "C123", Mentions: nil}, // explicitly empty mentions
		},
	}
	messenger := &fakeMessenger{}
	h := newOpenHandler(store, resolver, messenger)

	e := kernel.Event{
		Action:     kernel.ActionOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "bump acme/lib", URL: "u", Author: "dependabot[bot]"},
		Sender:     kernel.Sender{Login: "dependabot[bot]", Type: kernel.SenderTypeBot},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(messenger.opens) != 1 {
		t.Fatalf("want 1 PostOpen call; got %d", len(messenger.opens))
	}
	req := messenger.opens[0].req
	// The mentions slice should be nil/empty — the messenger is responsible for
	// rendering correctly; the handler must not inject stray values.
	if len(req.Mentions) != 0 {
		t.Errorf("Mentions should be empty; got %v", req.Mentions)
	}
}

func TestOpenHandler_Handle_DoesNotPersistOnSlackFailure(t *testing.T) {
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{
			DependabotFormat: true,
			Reactions:        routingdomain.Reactions{NewPR: "rocket"},
		},
		targets: []routingdomain.Target{
			{Channel: "C123"},
		},
	}
	messenger := &fakeMessenger{postErr: errors.New("injected failure")}
	h := newOpenHandler(store, resolver, messenger)

	e := kernel.Event{
		Action:     kernel.ActionOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	err := h.Handle(context.Background(), e)
	if err == nil {
		t.Fatal("Handle should return error on PostOpen failure")
	}
	if _, storeErr := store.Messages(context.Background(), "octo/widget", 42); storeErr == nil {
		t.Error("message should not be saved when PostOpen fails")
	}
}

func TestOpenHandler_FansOutToEachTarget(t *testing.T) {
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{Reactions: routingdomain.Reactions{NewPR: "eyes"}},
		targets: []routingdomain.Target{
			{Channel: "C0A", Mentions: []string{"<@U0A>"}},
			{Channel: "C0B", Mentions: []string{"<@U0B>"}},
		},
	}
	messenger := &fakeMessenger{}
	h := application.NewOpenHandler(store, resolver, messenger, discardLogger())

	if err := h.Handle(context.Background(), openedEvent("acme/web", 7)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(messenger.opens) != 2 {
		t.Fatalf("want 2 PostOpen calls; got %d", len(messenger.opens))
	}
	channels := map[string]bool{}
	for _, o := range messenger.opens {
		channels[o.channel] = true
	}
	if !channels["C0A"] || !channels["C0B"] {
		t.Fatalf("want posts to C0A and C0B; got %v", channels)
	}
	msgs, _ := store.Messages(context.Background(), "acme/web", 7)
	if len(msgs) != 2 {
		t.Fatalf("want 2 stored messages; got %d", len(msgs))
	}
}
