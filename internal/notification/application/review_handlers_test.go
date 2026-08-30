package application_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// reviewBehavior returns a fakeBehavior for octo/widget with the standard
// review reactions and the given IgnoreAIReviews / BotReview settings.
func reviewBehavior(ignoreAIReviews bool, botReview string) *fakeBehavior {
	return &fakeBehavior{mapping: routingdomain.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: ignoreAIReviews,
		Reactions: routingdomain.Reactions{
			Approved:      "white_check_mark",
			Commented:     "speech_balloon",
			RequestChange: "exclamation",
			BotReview:     botReview,
		},
	}}
}

// setupReviewFixture seeds one stored message (channel C123) for octo/widget#42
// and returns the store, a default behavior, and a fresh messenger.
func setupReviewFixture(t *testing.T) (*fakeMessageStore, *fakeBehavior, *fakeMessenger) {
	t.Helper()

	return storeWithMessage("octo/widget", 42), reviewBehavior(false, ""), &fakeMessenger{}
}

// noActiveSession returns a fakeReviewSessions preset to report no active session.
func noActiveSession() *fakeReviewSessions {
	return &fakeReviewSessions{activeErr: domain.ErrNoActiveReview}
}

func botSender(login string) kernel.Sender {
	return kernel.Sender{Login: login, IsBot: true}
}

func reviewEventBy(kind kernel.EventKind, sender kernel.Sender) kernel.Event {
	return kernel.Event{
		Kind:       kind,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     sender,
	}
}

func reviewEvent(kind kernel.EventKind) kernel.Event {
	return reviewEventBy(kind, kernel.Sender{})
}

// submittedReviewEvent is an approved review carrying the full PR object the
// message recompose depends on (title/url/author come from the webhook).
func submittedReviewEvent() kernel.Event {
	return kernel.Event{
		Kind:       kernel.KindApproved,
		Repository: "octo/widget",
		PR: kernel.PR{
			Number: 42,
			Title:  "Add widget",
			URL:    "https://github.com/octo/widget/pull/42",
			Author: "alice",
		},
	}
}

func TestApproveHandler_Applicable(t *testing.T) {
	handler := application.NewApproveHandler(nil, nil, nil, discardLogger(), noActiveSession())

	assert.True(t, handler.Applicable(kernel.Event{Kind: kernel.KindApproved}))
	assert.False(t, handler.Applicable(kernel.Event{Kind: kernel.KindReviewCommented}))
	assert.False(t, handler.Applicable(kernel.Event{Kind: kernel.KindUnknown}))
}

func TestApproveHandler_Handle_AddsReaction(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	handler := application.NewApproveHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEvent(kernel.KindApproved))

	require.NoError(t, err)
	require.Len(t, messenger.reactions, 1)
	assert.Equal(t, "white_check_mark", messenger.reactions[0].emoji)
	assert.Equal(t, "C123", messenger.reactions[0].channel)
	assert.Equal(t, "ts1", messenger.reactions[0].messageID)
}

func TestApproveHandler_Handle_TouchesActivity(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	handler := application.NewApproveHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEvent(kernel.KindApproved))

	require.NoError(t, err)
	assert.Equal(t, 1, store.touched[storeKey("octo/widget", 42)], "a review resets the idle clock")
}

func TestApproveHandler_IgnoreAIReviews_BotSenderDoesNotTouch(t *testing.T) {
	store := storeWithMessage("octo/widget", 42)
	messenger := &fakeMessenger{}
	handler := application.NewApproveHandler(store, reviewBehavior(true, ""), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindApproved, botSender("copilot[bot]")))

	require.NoError(t, err)
	assert.Zero(t, store.touched[storeKey("octo/widget", 42)], "a suppressed AI review must not reset the idle clock")
	assert.Empty(t, messenger.reactionEmojis())
}

func TestCommentedHandler_Applicable(t *testing.T) {
	handler := application.NewCommentedHandler(nil, nil, nil, discardLogger(), noActiveSession())

	testCases := []struct {
		name string
		kind kernel.EventKind
		want bool
	}{
		{"comment (line/conversation/edited-review)", kernel.KindCommented, true},
		{"submitted commented review", kernel.KindReviewCommented, true},
		{"approved review", kernel.KindApproved, false},
		{"changes requested", kernel.KindChangesRequested, false},
		{"unmapped event", kernel.KindUnknown, false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, handler.Applicable(kernel.Event{Kind: testCase.kind}))
		})
	}
}

func TestCommentedHandler_Handle_AddsReaction(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	handler := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEvent(kernel.KindReviewCommented))

	require.NoError(t, err)
	assert.Equal(t, []string{"speech_balloon"}, messenger.reactionEmojis())
}

func TestCommentedHandler_Handle_LineCommentAddsReaction(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	handler := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEvent(kernel.KindCommented))

	require.NoError(t, err)
	assert.Equal(t, []string{"speech_balloon"}, messenger.reactionEmojis())
}

func TestRequestChangeHandler_Applicable(t *testing.T) {
	handler := application.NewRequestChangeHandler(nil, nil, nil, discardLogger(), noActiveSession())

	assert.True(t, handler.Applicable(kernel.Event{Kind: kernel.KindChangesRequested}))
	assert.False(t, handler.Applicable(kernel.Event{Kind: kernel.KindCommented}))
	assert.False(t, handler.Applicable(kernel.Event{Kind: kernel.KindUnknown}))
}

func TestRequestChangeHandler_Handle_AddsReaction(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	handler := application.NewRequestChangeHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEvent(kernel.KindChangesRequested))

	require.NoError(t, err)
	assert.Equal(t, []string{"exclamation"}, messenger.reactionEmojis())
}

func TestReactionHandler_ReactsOnEveryMessage(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42,
		domain.Message{Channel: "C0A", MessageID: "ts-a"},
		domain.Message{Channel: "C0B", MessageID: "ts-b"},
	)
	messenger := &fakeMessenger{}
	handler := application.NewApproveHandler(store, reviewBehavior(false, ""), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEvent(kernel.KindApproved))

	require.NoError(t, err)
	assert.Len(t, messenger.reactions, 2, "one reaction per stored message")
	assert.Equal(t, 1, store.touched[storeKey("octo/widget", 42)], "the PR is touched once, not once per message")
}

func TestApproveHandler_IgnoreAIReviews_BotSenderSuppressesReaction(t *testing.T) {
	messenger := &fakeMessenger{}
	handler := application.NewApproveHandler(storeWithMessage("octo/widget", 42), reviewBehavior(true, ""), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindApproved, botSender("copilot[bot]")))

	require.NoError(t, err)
	assert.Empty(t, messenger.reactionEmojis())
}

func TestApproveHandler_IgnoreAIReviews_HumanSenderReacts(t *testing.T) {
	messenger := &fakeMessenger{}
	handler := application.NewApproveHandler(storeWithMessage("octo/widget", 42), reviewBehavior(true, ""), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindApproved, kernel.Sender{Login: "alice"}))

	require.NoError(t, err)
	assert.Equal(t, []string{"white_check_mark"}, messenger.reactionEmojis(), "suppression applies to bots only")
}

func TestApproveHandler_IgnoreAIReviewsFalse_BotSenderStillReacts(t *testing.T) {
	messenger := &fakeMessenger{}
	handler := application.NewApproveHandler(storeWithMessage("octo/widget", 42), reviewBehavior(false, ""), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindApproved, botSender("dependabot[bot]")))

	require.NoError(t, err)
	assert.Equal(t, []string{"white_check_mark"}, messenger.reactionEmojis())
}

func TestCommentedHandler_IgnoreAIReviews_BotSenderSuppressesReaction(t *testing.T) {
	messenger := &fakeMessenger{}
	handler := application.NewCommentedHandler(storeWithMessage("octo/widget", 42), reviewBehavior(true, ""), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindReviewCommented, botSender("claude[bot]")))

	require.NoError(t, err)
	assert.Empty(t, messenger.reactionEmojis())
}

func TestCommentedHandler_IgnoreAIReviews_BotLineCommentSuppressed(t *testing.T) {
	messenger := &fakeMessenger{}
	handler := application.NewCommentedHandler(storeWithMessage("octo/widget", 42), reviewBehavior(true, ""), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindCommented, botSender("github-actions[bot]")))

	require.NoError(t, err)
	assert.Empty(t, messenger.reactionEmojis())
}

func TestRequestChangeHandler_IgnoreAIReviews_BotSenderSuppressesReaction(t *testing.T) {
	messenger := &fakeMessenger{}
	handler := application.NewRequestChangeHandler(storeWithMessage("octo/widget", 42), reviewBehavior(true, ""), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindChangesRequested, botSender("release-please[bot]")))

	require.NoError(t, err)
	assert.Empty(t, messenger.reactionEmojis())
}

func TestReactionHandler_SuppressedReactionLogsAtDebug(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := application.NewApproveHandler(storeWithMessage("octo/widget", 42), reviewBehavior(true, ""), &fakeMessenger{}, logger, noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindApproved, botSender("copilot[bot]")))

	require.NoError(t, err)
	assert.Contains(t, logged.String(), "level=DEBUG")
	assert.Contains(t, logged.String(), "copilot[bot]")
}

func TestCommentedHandler_BotMarker_AddsMarkerAlongsideStateReaction(t *testing.T) {
	messenger := &fakeMessenger{}
	handler := application.NewCommentedHandler(storeWithMessage("octo/widget", 42), reviewBehavior(false, "robot_face"), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindReviewCommented, botSender("copilot[bot]")))

	require.NoError(t, err)
	assert.Equal(t, []string{"speech_balloon", "robot_face"}, messenger.reactionEmojis())
}

func TestApproveHandler_BotMarker_AddsMarkerAlongsideStateReaction(t *testing.T) {
	messenger := &fakeMessenger{}
	handler := application.NewApproveHandler(storeWithMessage("octo/widget", 42), reviewBehavior(false, "robot_face"), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindApproved, botSender("dependabot[bot]")))

	require.NoError(t, err)
	assert.Equal(t, []string{"white_check_mark", "robot_face"}, messenger.reactionEmojis())
}

func TestCommentedHandler_BotMarker_LineCommentBotGetsMarker(t *testing.T) {
	messenger := &fakeMessenger{}
	handler := application.NewCommentedHandler(storeWithMessage("octo/widget", 42), reviewBehavior(false, "robot_face"), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindCommented, botSender("github-actions[bot]")))

	require.NoError(t, err)
	assert.Equal(t, []string{"speech_balloon", "robot_face"}, messenger.reactionEmojis())
}

func TestCommentedHandler_BotMarker_HumanGetsOnlyStateReaction(t *testing.T) {
	messenger := &fakeMessenger{}
	handler := application.NewCommentedHandler(storeWithMessage("octo/widget", 42), reviewBehavior(false, "robot_face"), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindReviewCommented, kernel.Sender{Login: "alice"}))

	require.NoError(t, err)
	assert.Equal(t, []string{"speech_balloon"}, messenger.reactionEmojis())
}

func TestCommentedHandler_BotMarker_SuppressedBotGetsNothing(t *testing.T) {
	messenger := &fakeMessenger{}
	handler := application.NewCommentedHandler(storeWithMessage("octo/widget", 42), reviewBehavior(true, "robot_face"), messenger, discardLogger(), noActiveSession())

	err := handler.Handle(context.Background(), reviewEventBy(kernel.KindReviewCommented, botSender("copilot[bot]")))

	require.NoError(t, err)
	assert.Empty(t, messenger.reactionEmojis(), "suppression wins over the marker")
}

func TestApproveHandler_SubmittedReview_FinishesSession(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := noActiveSession()
	handler := application.NewApproveHandler(store, behavior, messenger, discardLogger(), reviews)

	err := handler.Handle(context.Background(), reviewEvent(kernel.KindApproved))

	require.NoError(t, err)
	assert.Equal(t, 1, reviews.finished)
	assert.Equal(t, 1, store.touched[storeKey("octo/widget", 42)])
	assert.Len(t, messenger.reactions, 1)
}

func TestRequestChangeHandler_SubmittedReview_FinishesSession(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := noActiveSession()
	handler := application.NewRequestChangeHandler(store, behavior, messenger, discardLogger(), reviews)

	err := handler.Handle(context.Background(), reviewEvent(kernel.KindChangesRequested))

	require.NoError(t, err)
	assert.Equal(t, 1, reviews.finished)
}

func TestCommentedHandler_LineComment_DoesNotFinishSession(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := noActiveSession()
	handler := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), reviews)

	// A line comment and a conversation comment both map to KindCommented.
	err := handler.Handle(context.Background(), reviewEvent(kernel.KindCommented))

	require.NoError(t, err)
	assert.Zero(t, reviews.finished, "only a submitted review ends the session")
}

func TestCommentedHandler_SubmittedCommentReview_FinishesSession(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := noActiveSession()
	handler := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), reviews)

	err := handler.Handle(context.Background(), reviewEvent(kernel.KindReviewCommented))

	require.NoError(t, err)
	assert.Equal(t, 1, reviews.finished)
}

func TestApproveHandler_SubmittedReview_ActiveSession_ClearsInReviewState(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := &fakeReviewSessions{
		active:    domain.ReviewSession{SlackUserID: "U1"},
		reviewers: []domain.ReviewSession{{SlackUserID: "U1"}},
	}
	handler := application.NewApproveHandler(store, behavior, messenger, discardLogger(), reviews)

	err := handler.Handle(context.Background(), submittedReviewEvent())

	require.NoError(t, err)
	require.Len(t, messenger.reviewFinished, 1)
	assert.Equal(t, []string{"U1"}, messenger.reviewFinished[0].req.ReviewerIDs)
	assert.Equal(t, 1, reviews.finished)
	assert.Len(t, messenger.reactions, 1)
}

func TestApproveHandler_SubmittedReview_NoActiveSession_LeavesMessageUntouched(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := noActiveSession()
	handler := application.NewApproveHandler(store, behavior, messenger, discardLogger(), reviews)

	err := handler.Handle(context.Background(), submittedReviewEvent())

	require.NoError(t, err)
	assert.Empty(t, messenger.reviewFinished, "with nobody reviewing there is no in-review state to clear")
	assert.Len(t, messenger.reactions, 1)
	assert.Equal(t, 1, reviews.finished, "Finish is idempotent and still called")
}

func TestReactionHandler_SubmittedReview_ActiveSession_UpdatesEveryStoredMessage(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42,
		domain.Message{Channel: "C0A", MessageID: "ts-a"},
		domain.Message{Channel: "C0B", MessageID: "ts-b"},
	)
	messenger := &fakeMessenger{}
	reviews := &fakeReviewSessions{
		active:    domain.ReviewSession{SlackUserID: "U1"},
		reviewers: []domain.ReviewSession{{SlackUserID: "U1"}, {SlackUserID: "U2"}},
	}
	handler := application.NewApproveHandler(store, reviewBehavior(false, ""), messenger, discardLogger(), reviews)

	err := handler.Handle(context.Background(), submittedReviewEvent())

	require.NoError(t, err)
	require.Len(t, messenger.reviewFinished, 2, "one update per stored message")
	assert.Equal(t, []string{"U1", "U2"}, messenger.reviewFinished[0].req.ReviewerIDs)
}

func TestApproveHandler_SubmittedReview_ReviewersLoadError_StillClearsInReviewState(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := &fakeReviewSessions{
		active:       domain.ReviewSession{SlackUserID: "U1"},
		reviewers:    []domain.ReviewSession{{SlackUserID: "U1"}},
		reviewersErr: errInjected,
	}
	handler := application.NewApproveHandler(store, behavior, messenger, discardLogger(), reviews)

	err := handler.Handle(context.Background(), submittedReviewEvent())

	require.NoError(t, err, "a reviewers-load error soft-degrades")
	require.Len(t, messenger.reviewFinished, 1)
	assert.Empty(t, messenger.reviewFinished[0].req.ReviewerIDs)
}

func TestApproveHandler_SubmittedReview_GetActiveError_Fails(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := &fakeReviewSessions{activeErr: errInjected}
	handler := application.NewApproveHandler(store, behavior, messenger, discardLogger(), reviews)

	err := handler.Handle(context.Background(), submittedReviewEvent())

	assert.ErrorIs(t, err, errInjected, "a non-NotFound GetActive error must surface")
}

// reviewHandlerFactory is the shared constructor signature of the three
// reaction handlers.
type reviewHandlerFactory func(domain.MessageStore, domain.RepoBehavior, domain.Messenger, *slog.Logger, domain.ReviewSessions) domain.Handler

func TestReviewHandlers_NoStoredMessageIsNoop(t *testing.T) {
	testCases := []struct {
		name       string
		newHandler reviewHandlerFactory
		event      kernel.Event
	}{
		{
			name: "approve",
			newHandler: func(store domain.MessageStore, behavior domain.RepoBehavior, messenger domain.Messenger, logger *slog.Logger, reviews domain.ReviewSessions) domain.Handler {
				return application.NewApproveHandler(store, behavior, messenger, logger, reviews)
			},
			event: reviewEvent(kernel.KindApproved),
		},
		{
			name: "commented",
			newHandler: func(store domain.MessageStore, behavior domain.RepoBehavior, messenger domain.Messenger, logger *slog.Logger, reviews domain.ReviewSessions) domain.Handler {
				return application.NewCommentedHandler(store, behavior, messenger, logger, reviews)
			},
			event: reviewEvent(kernel.KindReviewCommented),
		},
		{
			name: "request_change",
			newHandler: func(store domain.MessageStore, behavior domain.RepoBehavior, messenger domain.Messenger, logger *slog.Logger, reviews domain.ReviewSessions) domain.Handler {
				return application.NewRequestChangeHandler(store, behavior, messenger, logger, reviews)
			},
			event: reviewEvent(kernel.KindChangesRequested),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			messenger := &fakeMessenger{}
			handler := testCase.newHandler(newFakeMessageStore(), reviewBehavior(false, ""), messenger, discardLogger(), noActiveSession())

			err := handler.Handle(context.Background(), testCase.event)

			require.NoError(t, err)
			assert.Empty(t, messenger.reactionEmojis(), "nothing stored means nothing to react to")
		})
	}
}
