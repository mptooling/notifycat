package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

func newCloseHandler(
	store *fakeMessageStore,
	behavior *fakeBehavior,
	messenger *fakeMessenger,
	reviews *fakeReviewSessions,
) *application.CloseHandler {
	return application.NewCloseHandler(store, behavior, messenger, discardLogger(), reviews)
}

// closeBehavior is the repo behavior close-handler tests run under: the merged
// and closed emoji differ so tests can tell which branch ran.
func closeBehavior(reactionsEnabled bool) *fakeBehavior {
	return &fakeBehavior{mapping: routingdomain.RepoMapping{
		Reactions: routingdomain.Reactions{
			Enabled:  reactionsEnabled,
			MergedPR: "twisted_rightwards_arrows",
			ClosedPR: "x",
		},
	}}
}

func storeWithMessage(repository string, prNumber int) *fakeMessageStore {
	store := newFakeMessageStore()
	store.seed(repository, prNumber, domain.Message{Channel: "C123", MessageID: "ts1"})
	return store
}

func closedMergedEvent(repository string, prNumber int) kernel.Event {
	return kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindMerged,
		Repository: repository,
		PR:         kernel.PR{Number: prNumber, Title: "fix", URL: "u", Author: "a", Merged: true},
	}
}

func closedNotMergedEvent(repository string, prNumber int) kernel.Event {
	return kernel.Event{
		Provider:   kernel.ProviderGitHub,
		Kind:       kernel.KindClosed,
		Repository: repository,
		PR:         kernel.PR{Number: prNumber},
	}
}

func TestCloseHandler_Applicable(t *testing.T) {
	handler := newCloseHandler(newFakeMessageStore(), &fakeBehavior{}, &fakeMessenger{}, &fakeReviewSessions{})

	assert.True(t, handler.Applicable(kernel.Event{Kind: kernel.KindClosed}))
	assert.False(t, handler.Applicable(kernel.Event{Kind: kernel.KindOpened}))
}

func TestCloseHandler_Handle_UpdatesMessage(t *testing.T) {
	store := storeWithMessage("octo/widget", 42)
	messenger := &fakeMessenger{}
	handler := newCloseHandler(store, closeBehavior(true), messenger, &fakeReviewSessions{})

	err := handler.Handle(context.Background(), closedMergedEvent("octo/widget", 42))

	require.NoError(t, err)
	require.Len(t, messenger.closes, 1)
	assert.True(t, messenger.closes[0].req.Merged)
	assert.Equal(t, "twisted_rightwards_arrows", messenger.closes[0].req.Emoji)
	assert.Equal(t, []string{"twisted_rightwards_arrows"}, messenger.reactionEmojis())
}

func TestCloseHandler_Handle_ClosedNotMergedUsesClosedEmoji(t *testing.T) {
	store := storeWithMessage("octo/widget", 42)
	messenger := &fakeMessenger{}
	handler := newCloseHandler(store, closeBehavior(true), messenger, &fakeReviewSessions{})

	err := handler.Handle(context.Background(), closedNotMergedEvent("octo/widget", 42))

	require.NoError(t, err)
	require.Len(t, messenger.closes, 1)
	assert.Equal(t, "x", messenger.closes[0].req.Emoji)
	assert.Equal(t, []string{"x"}, messenger.reactionEmojis())
}

func TestCloseHandler_Handle_NoReactionWhenDisabled(t *testing.T) {
	store := storeWithMessage("octo/widget", 42)
	messenger := &fakeMessenger{}
	handler := newCloseHandler(store, closeBehavior(false), messenger, &fakeReviewSessions{})

	err := handler.Handle(context.Background(), closedMergedEvent("octo/widget", 42))

	require.NoError(t, err)
	assert.Empty(t, messenger.reactionEmojis())
}

func TestCloseHandler_Handle_MarksClosed(t *testing.T) {
	store := storeWithMessage("octo/widget", 42)
	// Reactions disabled to prove MarkClosed is independent of reactions.
	handler := newCloseHandler(store, closeBehavior(false), &fakeMessenger{}, &fakeReviewSessions{})

	err := handler.Handle(context.Background(), closedMergedEvent("octo/widget", 42))

	require.NoError(t, err)
	assert.True(t, store.closed[storeKey("octo/widget", 42)])
}

func TestCloseHandler_Handle_NoStoredMessageIsNoop(t *testing.T) {
	store := newFakeMessageStore()
	messenger := &fakeMessenger{}
	handler := newCloseHandler(store, closeBehavior(true), messenger, &fakeReviewSessions{})

	err := handler.Handle(context.Background(), closedNotMergedEvent("octo/widget", 42))

	require.NoError(t, err)
	assert.Empty(t, messenger.closes)
	assert.Empty(t, messenger.reactions)
	assert.False(t, store.closed[storeKey("octo/widget", 42)], "an untracked PR is never marked closed")
}

func TestCloseHandler_ActsOnEveryMessage(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("acme/web", 7,
		domain.Message{Channel: "C0A", MessageID: "100.1"},
		domain.Message{Channel: "C0B", MessageID: "200.1"},
	)
	behavior := &fakeBehavior{mapping: routingdomain.RepoMapping{
		Reactions: routingdomain.Reactions{Enabled: true, MergedPR: "tada"},
	}}
	messenger := &fakeMessenger{}
	handler := newCloseHandler(store, behavior, messenger, &fakeReviewSessions{})

	err := handler.Handle(context.Background(), closedMergedEvent("acme/web", 7))

	require.NoError(t, err)
	assert.Len(t, messenger.closes, 2)
	assert.Len(t, messenger.reactions, 2)
}

func TestCloseHandler_ReviewedByOnClose(t *testing.T) {
	store := storeWithMessage("octo/widget", 42)
	messenger := &fakeMessenger{}
	reviews := &fakeReviewSessions{
		reviewers: []domain.ReviewSession{
			{SlackUserID: "U1", SlackUserName: "Alice"},
			{SlackUserID: "U2", SlackUserName: "Bob"},
		},
	}
	handler := newCloseHandler(store, closeBehavior(false), messenger, reviews)

	err := handler.Handle(context.Background(), closedMergedEvent("octo/widget", 42))

	require.NoError(t, err)
	require.Len(t, messenger.closes, 1)
	assert.ElementsMatch(t, []string{"U1", "U2"}, messenger.closes[0].req.ReviewerIDs)
}

func TestCloseHandler_NoReviewersNoReviewedByBlock(t *testing.T) {
	store := storeWithMessage("octo/widget", 42)
	messenger := &fakeMessenger{}
	handler := newCloseHandler(store, closeBehavior(false), messenger, &fakeReviewSessions{})

	err := handler.Handle(context.Background(), closedMergedEvent("octo/widget", 42))

	require.NoError(t, err)
	require.Len(t, messenger.closes, 1)
	assert.Empty(t, messenger.closes[0].req.ReviewerIDs)
}

func TestCloseHandler_ReviewedByDedup(t *testing.T) {
	store := storeWithMessage("octo/widget", 42)
	messenger := &fakeMessenger{}
	reviews := &fakeReviewSessions{
		reviewers: []domain.ReviewSession{
			{SlackUserID: "U1", SlackUserName: "Alice"},
			{SlackUserID: "U1", SlackUserName: "Alice"},
			{SlackUserID: "U2", SlackUserName: "Bob"},
		},
	}
	handler := newCloseHandler(store, closeBehavior(false), messenger, reviews)

	err := handler.Handle(context.Background(), closedMergedEvent("octo/widget", 42))

	require.NoError(t, err)
	require.Len(t, messenger.closes, 1)
	assert.ElementsMatch(t, []string{"U1", "U2"}, messenger.closes[0].req.ReviewerIDs, "a repeat reviewer is listed once")
}

func TestCloseHandler_FinishesSessionOnClose(t *testing.T) {
	store := storeWithMessage("octo/widget", 42)
	reviews := &fakeReviewSessions{}
	handler := newCloseHandler(store, closeBehavior(false), &fakeMessenger{}, reviews)

	err := handler.Handle(context.Background(), closedMergedEvent("octo/widget", 42))

	require.NoError(t, err)
	assert.Equal(t, 1, reviews.finished, "closing a PR ends any active review session")
	assert.True(t, store.closed[storeKey("octo/widget", 42)])
}

func TestCloseHandler_ReviewersLoadFailureSoftDegrades(t *testing.T) {
	store := storeWithMessage("octo/widget", 42)
	messenger := &fakeMessenger{}
	reviews := &fakeReviewSessions{reviewersErr: errInjected}
	handler := newCloseHandler(store, closeBehavior(false), messenger, reviews)

	err := handler.Handle(context.Background(), closedMergedEvent("octo/widget", 42))

	require.NoError(t, err, "a reviewer-load failure must not fail the close")
	require.Len(t, messenger.closes, 1)
	assert.Empty(t, messenger.closes[0].req.ReviewerIDs)
	assert.True(t, store.closed[storeKey("octo/widget", 42)])
}
