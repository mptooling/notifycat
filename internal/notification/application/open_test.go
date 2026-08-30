package application_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// dependabotFormatting is the repo behavior most open-handler tests run under:
// the compact bot format is on and a new PR gets the rocket reaction.
var dependabotFormatting = routingdomain.RepoMapping{
	DependabotFormat: true,
	Reactions:        routingdomain.Reactions{NewPR: "rocket"},
}

func targetsFor(channels ...string) []routingdomain.Target {
	targets := make([]routingdomain.Target, len(channels))
	for i, channel := range channels {
		targets[i] = routingdomain.Target{Channel: channel}
	}
	return targets
}

func newOpenHandler(
	store *fakeMessageStore,
	resolver *fakeTargetResolver,
	messenger *fakeMessenger,
) *application.OpenHandler {
	return application.NewOpenHandler(store, resolver, messenger, discardLogger())
}

func openedEvent(repository string, prNumber int) kernel.Event {
	return kernel.Event{
		Kind:       kernel.KindOpened,
		Repository: repository,
		PR:         kernel.PR{Number: prNumber, Title: fmt.Sprintf("PR #%d", prNumber)},
	}
}

// requireSingleOpen asserts exactly one PostOpen happened and returns its request.
func requireSingleOpen(t *testing.T, messenger *fakeMessenger) domain.OpenRequest {
	t.Helper()

	require.Len(t, messenger.opens, 1)
	return messenger.opens[0].req
}

func TestOpenHandler_Applicable(t *testing.T) {
	handler := newOpenHandler(newFakeMessageStore(), &fakeTargetResolver{}, &fakeMessenger{})

	testCases := []struct {
		name  string
		event kernel.Event
		want  bool
	}{
		{"opened non-draft", kernel.Event{Kind: kernel.KindOpened}, true},
		{"ready_for_review", kernel.Event{Kind: kernel.KindReadyForReview}, true},
		{"closed", kernel.Event{Kind: kernel.KindClosed}, false},
		{"submitted approved", kernel.Event{Kind: kernel.KindApproved}, false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, handler.Applicable(testCase.event))
		})
	}
}

func TestOpenHandler_Handle_PostsAndStoresTS(t *testing.T) {
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{
		behavior: dependabotFormatting,
		targets:  []routingdomain.Target{{Channel: "C123", Mentions: []string{"@alice"}}},
	}
	messenger := &fakeMessenger{}
	handler := newOpenHandler(store, resolver, messenger)
	event := kernel.Event{
		Kind:       kernel.KindOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "fix", URL: "u", Author: "a"},
	}

	err := handler.Handle(context.Background(), event)

	require.NoError(t, err)
	require.Len(t, messenger.opens, 1)
	assert.Equal(t, "C123", messenger.opens[0].channel)

	messages, err := store.Messages(context.Background(), "octo/widget", 42)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "C123", messages[0].Channel)
	assert.NotEmpty(t, messages[0].MessageID)
}

func TestOpenHandler_Handle_ForwardsPRFieldsToMessenger(t *testing.T) {
	// Rendering the context line and fallback is a messenger concern; the handler
	// only has to forward the PR fields intact.
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{
		behavior: dependabotFormatting,
		targets:  []routingdomain.Target{{Channel: "C123", Mentions: []string{"@alice"}}},
	}
	messenger := &fakeMessenger{}
	handler := newOpenHandler(store, resolver, messenger)
	event := kernel.Event{
		Kind:       kernel.KindOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "fix", URL: "u", Author: "alice"},
	}

	err := handler.Handle(context.Background(), event)

	require.NoError(t, err)
	request := requireSingleOpen(t, messenger)
	assert.Equal(t, "octo/widget", request.Repository)
	assert.Equal(t, 42, request.PR.Number)
	assert.Equal(t, "alice", request.PR.Author)
}

func TestOpenHandler_Handle_SkipsIfMessageAlreadyExists(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "preexisting-ts"})
	resolver := &fakeTargetResolver{behavior: dependabotFormatting, targets: targetsFor("C123")}
	messenger := &fakeMessenger{}
	handler := newOpenHandler(store, resolver, messenger)

	err := handler.Handle(context.Background(), openedEvent("octo/widget", 42))

	require.NoError(t, err)
	assert.Empty(t, messenger.opens, "one message per (PR, channel)")
}

func TestOpenHandler_Handle_SkipsIfNoMapping(t *testing.T) {
	messenger := &fakeMessenger{}
	handler := newOpenHandler(newFakeMessageStore(), &fakeTargetResolver{err: routingdomain.ErrNotFound}, messenger)

	err := handler.Handle(context.Background(), openedEvent("octo/widget", 42))

	require.NoError(t, err, "an unmapped repo is a silent no-op")
	assert.Empty(t, messenger.opens)
}

func TestOpenHandler_Handle_DependabotRoutine(t *testing.T) {
	messenger := &fakeMessenger{}
	resolver := &fakeTargetResolver{
		behavior: dependabotFormatting,
		targets:  []routingdomain.Target{{Channel: "C123", Mentions: []string{"@alice"}}},
	}
	handler := newOpenHandler(newFakeMessageStore(), resolver, messenger)
	event := kernel.Event{
		Kind:       kernel.KindOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "bump acme/lib from 1.2.0 to 1.2.1", URL: "u", Author: "dependabot[bot]"},
		Sender:     kernel.Sender{Login: "dependabot[bot]", IsBot: true},
	}

	err := handler.Handle(context.Background(), event)

	require.NoError(t, err)
	request := requireSingleOpen(t, messenger)
	require.NotNil(t, request.Bot)
	assert.Equal(t, "dependabot", request.Bot.Name)
	assert.False(t, request.Bot.Security)
}

func TestOpenHandler_Handle_DependabotSecurity(t *testing.T) {
	messenger := &fakeMessenger{}
	resolver := &fakeTargetResolver{behavior: dependabotFormatting, targets: targetsFor("C123")}
	handler := newOpenHandler(newFakeMessageStore(), resolver, messenger)
	event := kernel.Event{
		Kind:       kernel.KindOpened,
		Repository: "octo/widget",
		PR: kernel.PR{
			Number: 42, Title: "bump acme/lib from 1.2.0 to 1.2.1", URL: "u", Author: "dependabot[bot]",
			Body: "Bumps acme/lib.\n\n## Vulnerabilities fixed\n\nCVE-2026-1234.",
		},
		Sender: kernel.Sender{Login: "dependabot[bot]", IsBot: true},
	}

	err := handler.Handle(context.Background(), event)

	require.NoError(t, err)
	request := requireSingleOpen(t, messenger)
	require.NotNil(t, request.Bot)
	assert.Equal(t, "dependabot", request.Bot.Name)
	assert.True(t, request.Bot.Security)
}

func TestOpenHandler_Handle_Renovate(t *testing.T) {
	messenger := &fakeMessenger{}
	resolver := &fakeTargetResolver{behavior: dependabotFormatting, targets: targetsFor("C123")}
	handler := newOpenHandler(newFakeMessageStore(), resolver, messenger)
	event := kernel.Event{
		Kind:       kernel.KindOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 7, Title: "Update acme/lib to v2", URL: "u", Author: "renovate[bot]"},
		Sender:     kernel.Sender{Login: "renovate[bot]", IsBot: true},
	}

	err := handler.Handle(context.Background(), event)

	require.NoError(t, err)
	request := requireSingleOpen(t, messenger)
	require.NotNil(t, request.Bot)
	assert.Equal(t, "renovate", request.Bot.Name)
}

func TestOpenHandler_Handle_DependabotReadyForReviewByHuman(t *testing.T) {
	// Regression: a draft Dependabot PR marked ready_for_review by a human. The
	// webhook sender is the human who clicked the button, so detection has to key
	// off the PR author or the compact format is lost.
	messenger := &fakeMessenger{}
	resolver := &fakeTargetResolver{behavior: dependabotFormatting, targets: targetsFor("C123")}
	handler := newOpenHandler(newFakeMessageStore(), resolver, messenger)
	event := kernel.Event{
		Kind:       kernel.KindReadyForReview,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "bump acme/lib from 1.2.0 to 1.2.1", URL: "u", Author: "dependabot[bot]"},
		Sender:     kernel.Sender{Login: "alice"},
	}

	err := handler.Handle(context.Background(), event)

	require.NoError(t, err)
	request := requireSingleOpen(t, messenger)
	require.NotNil(t, request.Bot, "detection keys off the PR author, not the sender")
	assert.Equal(t, "dependabot", request.Bot.Name)
}

func TestOpenHandler_Handle_DependabotFormatDisabled(t *testing.T) {
	messenger := &fakeMessenger{}
	resolver := &fakeTargetResolver{
		behavior: routingdomain.RepoMapping{DependabotFormat: false},
		targets:  targetsFor("C123"),
	}
	handler := newOpenHandler(newFakeMessageStore(), resolver, messenger)
	event := kernel.Event{
		Kind:       kernel.KindOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "bump acme/lib", URL: "u", Author: "dependabot[bot]"},
		Sender:     kernel.Sender{Login: "dependabot[bot]", IsBot: true},
	}

	err := handler.Handle(context.Background(), event)

	require.NoError(t, err)
	assert.Nil(t, requireSingleOpen(t, messenger).Bot, "the compact format is off, so the PR posts as a normal one")
}

func TestOpenHandler_Handle_DependabotEmptyMentions(t *testing.T) {
	messenger := &fakeMessenger{}
	resolver := &fakeTargetResolver{
		behavior: dependabotFormatting,
		targets:  []routingdomain.Target{{Channel: "C123", Mentions: nil}},
	}
	handler := newOpenHandler(newFakeMessageStore(), resolver, messenger)
	event := kernel.Event{
		Kind:       kernel.KindOpened,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42, Title: "bump acme/lib", URL: "u", Author: "dependabot[bot]"},
		Sender:     kernel.Sender{Login: "dependabot[bot]", IsBot: true},
	}

	err := handler.Handle(context.Background(), event)

	require.NoError(t, err)
	assert.Empty(t, requireSingleOpen(t, messenger).Mentions, "the handler never injects stray mentions")
}

func TestOpenHandler_Handle_DoesNotPersistOnSlackFailure(t *testing.T) {
	store := newFakeMessageStore()
	resolver := &fakeTargetResolver{behavior: dependabotFormatting, targets: targetsFor("C123")}
	messenger := &fakeMessenger{postErr: errInjected}
	handler := newOpenHandler(store, resolver, messenger)

	err := handler.Handle(context.Background(), openedEvent("octo/widget", 42))

	require.ErrorIs(t, err, errInjected)
	_, storeErr := store.Messages(context.Background(), "octo/widget", 42)
	assert.Error(t, storeErr, "nothing is persisted when the post fails")
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
	handler := newOpenHandler(store, resolver, messenger)

	err := handler.Handle(context.Background(), openedEvent("acme/web", 7))

	require.NoError(t, err)
	require.Len(t, messenger.opens, 2)
	assert.ElementsMatch(t, []string{"C0A", "C0B"}, []string{messenger.opens[0].channel, messenger.opens[1].channel})

	messages, err := store.Messages(context.Background(), "acme/web", 7)
	require.NoError(t, err)
	assert.Len(t, messages, 2)
}
