package application_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// reviewBehavior returns a fakeBehavior for octo/widget with the standard
// review reactions and the given IgnoreAIReviews / BotReview settings.
func reviewBehavior(ignoreAI bool, botReview string) *fakeBehavior {
	return &fakeBehavior{mapping: routingdomain.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: ignoreAI,
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
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	return store, reviewBehavior(false, ""), &fakeMessenger{}
}

// noActiveSession returns a fakeReviewSessions preset to report no active session.
func noActiveSession() *fakeReviewSessions {
	return &fakeReviewSessions{activeErr: domain.ErrNoActiveReview}
}

// ----- Approve -----

func TestApproveHandler_Applicable(t *testing.T) {
	h := application.NewApproveHandler(nil, nil, nil, discardLogger(), noActiveSession())

	if !h.Applicable(kernel.Event{Kind: kernel.KindApproved}) {
		t.Error("KindApproved should be applicable")
	}
	if h.Applicable(kernel.Event{Kind: kernel.KindReviewCommented}) {
		t.Error("a commented review should not be approve-applicable")
	}
	if h.Applicable(kernel.Event{Kind: kernel.KindUnknown}) {
		t.Error("an unmapped event should not be applicable")
	}
}

func TestApproveHandler_Handle_AddsReaction(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindApproved,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	emojis := messenger.reactionEmojis()
	if len(emojis) != 1 || emojis[0] != "white_check_mark" {
		t.Errorf("reactionEmojis = %v; want [white_check_mark]", emojis)
	}
	if messenger.reactions[0].channel != "C123" || messenger.reactions[0].messageID != "ts1" {
		t.Errorf("reaction target = (%q, %q); want (C123, ts1)", messenger.reactions[0].channel, messenger.reactions[0].messageID)
	}
}

func TestApproveHandler_Handle_TouchesActivity(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindApproved,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if store.touched[storeKey("octo/widget", 42)] != 1 {
		t.Fatalf("review activity not recorded via Touch: %d", store.touched[storeKey("octo/widget", 42)])
	}
}

func TestApproveHandler_IgnoreAIReviews_BotSenderDoesNotTouch(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(true, "")
	messenger := &fakeMessenger{}
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindApproved,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "copilot[bot]", IsBot: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if store.touched[storeKey("octo/widget", 42)] != 0 {
		t.Fatalf("suppressed AI review reset the idle clock via Touch: %d", store.touched[storeKey("octo/widget", 42)])
	}
	if len(messenger.reactions) != 0 {
		t.Fatalf("suppressed AI review should not call AddReaction: %v", messenger.reactionEmojis())
	}
}

// ----- Commented -----

func TestCommentedHandler_Applicable(t *testing.T) {
	h := application.NewCommentedHandler(nil, nil, nil, discardLogger(), noActiveSession())

	cases := []struct {
		name string
		e    kernel.Event
		want bool
	}{
		{"comment (line/conversation/edited-review)", kernel.Event{Kind: kernel.KindCommented}, true},
		{"submitted commented review", kernel.Event{Kind: kernel.KindReviewCommented}, true},
		{"approved review", kernel.Event{Kind: kernel.KindApproved}, false},
		{"changes requested", kernel.Event{Kind: kernel.KindChangesRequested}, false},
		{"unmapped event", kernel.Event{Kind: kernel.KindUnknown}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := h.Applicable(c.e); got != c.want {
				t.Errorf("Applicable = %v; want %v", got, c.want)
			}
		})
	}
}

func TestCommentedHandler_Handle_AddsReaction(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	h := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindReviewCommented,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	emojis := messenger.reactionEmojis()
	if len(emojis) != 1 || emojis[0] != "speech_balloon" {
		t.Fatalf("reactionEmojis = %v; want [speech_balloon]", emojis)
	}
}

func TestCommentedHandler_Handle_LineCommentAddsReaction(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	h := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindCommented,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	emojis := messenger.reactionEmojis()
	if len(emojis) != 1 || emojis[0] != "speech_balloon" {
		t.Fatalf("reactionEmojis = %v; want [speech_balloon]", emojis)
	}
}

// ----- RequestChange -----

func TestRequestChangeHandler_Applicable(t *testing.T) {
	h := application.NewRequestChangeHandler(nil, nil, nil, discardLogger(), noActiveSession())

	if !h.Applicable(kernel.Event{Kind: kernel.KindChangesRequested}) {
		t.Error("KindChangesRequested should be applicable")
	}
	if h.Applicable(kernel.Event{Kind: kernel.KindCommented}) {
		t.Error("a comment should not be request-change-applicable")
	}
	if h.Applicable(kernel.Event{Kind: kernel.KindUnknown}) {
		t.Error("an unmapped event should not be applicable")
	}
}

func TestRequestChangeHandler_Handle_AddsReaction(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	h := application.NewRequestChangeHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindChangesRequested,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	emojis := messenger.reactionEmojis()
	if len(emojis) != 1 || emojis[0] != "exclamation" {
		t.Fatalf("reactionEmojis = %v; want [exclamation]", emojis)
	}
}

// ----- Fan-out: react on every stored message -----

func TestReactionHandler_ReactsOnEveryMessage(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C0A", MessageID: "ts-a"})
	store.seed("octo/widget", 42, domain.Message{Channel: "C0B", MessageID: "ts-b"})
	behavior := reviewBehavior(false, "")
	messenger := &fakeMessenger{}
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindApproved,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.reactions) != 2 {
		t.Fatalf("want one reaction per stored message (2); got %d", len(messenger.reactions))
	}
	if store.touched[storeKey("octo/widget", 42)] != 1 {
		t.Fatalf("want exactly one Touch; got %d", store.touched[storeKey("octo/widget", 42)])
	}
}

// ----- Bot-reviewer suppression -----

func TestApproveHandler_IgnoreAIReviews_BotSenderSuppressesReaction(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(true, "")
	messenger := &fakeMessenger{}
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindApproved,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "copilot[bot]", IsBot: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.reactions) != 0 {
		t.Fatalf("AddReaction called for bot reviewer when IgnoreAIReviews=true: %v", messenger.reactionEmojis())
	}
}

func TestApproveHandler_IgnoreAIReviews_HumanSenderReacts(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(true, "")
	messenger := &fakeMessenger{}
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindApproved,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "alice"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.reactions) != 1 {
		t.Fatalf("human reviewer was incorrectly suppressed: %v", messenger.reactionEmojis())
	}
}

func TestApproveHandler_IgnoreAIReviewsFalse_BotSenderStillReacts(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(false, "")
	messenger := &fakeMessenger{}
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindApproved,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "dependabot[bot]", IsBot: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.reactions) != 1 {
		t.Fatalf("IgnoreAIReviews=false should allow bot reviewer: %v", messenger.reactionEmojis())
	}
}

func TestCommentedHandler_IgnoreAIReviews_BotSenderSuppressesReaction(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(true, "")
	messenger := &fakeMessenger{}
	h := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindReviewCommented,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "claude[bot]", IsBot: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.reactions) != 0 {
		t.Fatalf("AddReaction called for bot commenter: %v", messenger.reactionEmojis())
	}
}

func TestCommentedHandler_IgnoreAIReviews_BotLineCommentSuppressed(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(true, "")
	messenger := &fakeMessenger{}
	h := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindCommented,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "github-actions[bot]", IsBot: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.reactions) != 0 {
		t.Fatalf("AddReaction called for bot line-commenter: %v", messenger.reactionEmojis())
	}
}

func TestRequestChangeHandler_IgnoreAIReviews_BotSenderSuppressesReaction(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(true, "")
	messenger := &fakeMessenger{}
	h := application.NewRequestChangeHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindChangesRequested,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "release-please[bot]", IsBot: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.reactions) != 0 {
		t.Fatalf("AddReaction called for bot reviewer requesting changes: %v", messenger.reactionEmojis())
	}
}

func TestReactionHandler_SuppressedReactionLogsAtDebug(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(true, "")
	messenger := &fakeMessenger{}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := application.NewApproveHandler(store, behavior, messenger, logger, noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindApproved,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "copilot[bot]", IsBot: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("level=DEBUG")) {
		t.Errorf("expected DEBUG-level log; got: %q", out)
	}
	if !bytes.Contains(buf.Bytes(), []byte("copilot[bot]")) {
		t.Errorf("expected bot login in log; got: %q", out)
	}
}

// ----- Bot-reviewer marker (distinct reaction when NOT suppressed) -----

func TestCommentedHandler_BotMarker_AddsMarkerAlongsideStateReaction(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(false, "robot_face")
	messenger := &fakeMessenger{}
	h := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindReviewCommented,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "copilot[bot]", IsBot: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	emojis := messenger.reactionEmojis()
	if len(emojis) != 2 {
		t.Fatalf("want state reaction + bot marker; got emojis = %v", emojis)
	}
	if emojis[0] != "speech_balloon" || emojis[1] != "robot_face" {
		t.Errorf("reactions = %v; want [speech_balloon, robot_face]", emojis)
	}
}

func TestApproveHandler_BotMarker_AddsMarkerAlongsideStateReaction(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(false, "robot_face")
	messenger := &fakeMessenger{}
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindApproved,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "dependabot[bot]", IsBot: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	emojis := messenger.reactionEmojis()
	if len(emojis) != 2 || emojis[0] != "white_check_mark" || emojis[1] != "robot_face" {
		t.Fatalf("want [white_check_mark, robot_face]; got %v", emojis)
	}
}

func TestCommentedHandler_BotMarker_LineCommentBotGetsMarker(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(false, "robot_face")
	messenger := &fakeMessenger{}
	h := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindCommented,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "github-actions[bot]", IsBot: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	emojis := messenger.reactionEmojis()
	if len(emojis) != 2 || emojis[1] != "robot_face" {
		t.Fatalf("line-comment bot should also get the marker; got %v", emojis)
	}
}

func TestCommentedHandler_BotMarker_HumanGetsOnlyStateReaction(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(false, "robot_face")
	messenger := &fakeMessenger{}
	h := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindReviewCommented,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "alice"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	emojis := messenger.reactionEmojis()
	if len(emojis) != 1 || emojis[0] != "speech_balloon" {
		t.Fatalf("human reviewer should get only the state reaction; got %v", emojis)
	}
}

// Suppression wins over the marker: an ignored bot gets no reaction at all,
// not even the distinct marker.
func TestCommentedHandler_BotMarker_SuppressedBotGetsNothing(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C123", MessageID: "ts1"})
	behavior := reviewBehavior(true, "robot_face")
	messenger := &fakeMessenger{}
	h := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), noActiveSession())

	e := kernel.Event{
		Kind:       kernel.KindReviewCommented,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
		Sender:     kernel.Sender{Login: "copilot[bot]", IsBot: true},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.reactions) != 0 {
		t.Fatalf("ignored bot should get no reaction even with a marker set; got %v", messenger.reactionEmojis())
	}
}

// ----- Finish-on-submit -----

func TestApproveHandler_SubmittedReview_FinishesSession(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := noActiveSession()
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), reviews)

	e := kernel.Event{
		Kind:       kernel.KindApproved,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.finished != 1 {
		t.Fatalf("approved review should finish session; got finished=%d", reviews.finished)
	}
	if store.touched[storeKey("octo/widget", 42)] != 1 {
		t.Fatalf("Touch should still have been called; got %d", store.touched[storeKey("octo/widget", 42)])
	}
	if len(messenger.reactions) != 1 {
		t.Fatalf("reaction should still have been added; got %d", len(messenger.reactions))
	}
}

func TestRequestChangeHandler_SubmittedReview_FinishesSession(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := noActiveSession()
	h := application.NewRequestChangeHandler(store, behavior, messenger, discardLogger(), reviews)

	e := kernel.Event{
		Kind:       kernel.KindChangesRequested,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.finished != 1 {
		t.Fatalf("request-change review should finish session; got finished=%d", reviews.finished)
	}
}

func TestCommentedHandler_LineComment_DoesNotFinishSession(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := noActiveSession()
	h := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), reviews)

	e := kernel.Event{
		Kind:       kernel.KindCommented,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.finished != 0 {
		t.Fatalf("line comment should not finish session; got finished=%d", reviews.finished)
	}
}

func TestCommentedHandler_IssueComment_DoesNotFinishSession(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := noActiveSession()
	h := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), reviews)

	// A conversation comment on a PR also maps to KindCommented and must not
	// finish the review session.
	e := kernel.Event{
		Kind:       kernel.KindCommented,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.finished != 0 {
		t.Fatalf("issue comment should not finish session; got finished=%d", reviews.finished)
	}
}

func TestCommentedHandler_SubmittedCommentReview_FinishesSession(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := noActiveSession()
	h := application.NewCommentedHandler(store, behavior, messenger, discardLogger(), reviews)

	e := kernel.Event{
		Kind:       kernel.KindReviewCommented,
		Repository: "octo/widget",
		PR:         kernel.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.finished != 1 {
		t.Fatalf("submitted commented review should finish session; got finished=%d", reviews.finished)
	}
}

// ----- Submit takes the message out of the in-review state (AC #1) -----

// submittedReviewEvent is an approved review with the full PR object the
// recompose depends on (title/url/author come from the webhook).
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

func TestApproveHandler_SubmittedReview_ActiveSession_ClearsInReviewState(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := &fakeReviewSessions{
		active:    domain.ReviewSession{SlackUserID: "U1"},
		reviewers: []domain.ReviewSession{{SlackUserID: "U1"}},
	}
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), reviews)

	if err := h.Handle(context.Background(), submittedReviewEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.reviewFinished) != 1 {
		t.Fatalf("a submit with an active session should call UpdateReviewFinished once; got %d", len(messenger.reviewFinished))
	}
	if reviews.finished != 1 {
		t.Errorf("session should be finished; got finished=%d", reviews.finished)
	}
	if len(messenger.reactions) != 1 {
		t.Errorf("reaction should still be added; got %d", len(messenger.reactions))
	}
	req := messenger.reviewFinished[0].req
	if len(req.ReviewerIDs) != 1 || req.ReviewerIDs[0] != "U1" {
		t.Errorf("ReviewerIDs = %v; want [U1]", req.ReviewerIDs)
	}
}

func TestApproveHandler_SubmittedReview_NoActiveSession_LeavesMessageUntouched(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := noActiveSession() // nobody started a review
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), reviews)

	if err := h.Handle(context.Background(), submittedReviewEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.reviewFinished) != 0 {
		t.Fatalf("no active session should mean no UpdateReviewFinished call (reaction only); got %d", len(messenger.reviewFinished))
	}
	if len(messenger.reactions) != 1 {
		t.Fatalf("reaction should still be added; got %d", len(messenger.reactions))
	}
	if reviews.finished != 1 {
		t.Fatalf("Finish is idempotent and still called; got %d", reviews.finished)
	}
}

func TestReactionHandler_SubmittedReview_ActiveSession_UpdatesEveryStoredMessage(t *testing.T) {
	store := newFakeMessageStore()
	store.seed("octo/widget", 42, domain.Message{Channel: "C0A", MessageID: "ts-a"})
	store.seed("octo/widget", 42, domain.Message{Channel: "C0B", MessageID: "ts-b"})
	behavior := reviewBehavior(false, "")
	messenger := &fakeMessenger{}
	reviews := &fakeReviewSessions{
		active: domain.ReviewSession{SlackUserID: "U1"},
		reviewers: []domain.ReviewSession{
			{SlackUserID: "U1"},
			{SlackUserID: "U2"},
		},
	}
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), reviews)

	if err := h.Handle(context.Background(), submittedReviewEvent()); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(messenger.reviewFinished) != 2 {
		t.Fatalf("want one UpdateReviewFinished per stored message (2); got %d", len(messenger.reviewFinished))
	}
	ids := messenger.reviewFinished[0].req.ReviewerIDs
	if len(ids) != 2 || ids[0] != "U1" || ids[1] != "U2" {
		t.Errorf("ReviewerIDs = %v; want every reviewer listed", ids)
	}
}

func TestApproveHandler_SubmittedReview_ReviewersLoadError_StillClearsInReviewState(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := &fakeReviewSessions{
		active:       domain.ReviewSession{SlackUserID: "U1"},
		reviewers:    []domain.ReviewSession{{SlackUserID: "U1"}},
		reviewersErr: errInjected,
	}
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), reviews)

	if err := h.Handle(context.Background(), submittedReviewEvent()); err != nil {
		t.Fatalf("a reviewers-load error should soft-degrade, not fail Handle: %v", err)
	}
	if len(messenger.reviewFinished) != 1 {
		t.Fatalf("message should still leave the in-review state; got %d UpdateReviewFinished calls", len(messenger.reviewFinished))
	}
	req := messenger.reviewFinished[0].req
	if len(req.ReviewerIDs) != 0 {
		t.Errorf("reviewed-by IDs should be empty on load error; got %v", req.ReviewerIDs)
	}
}

func TestApproveHandler_SubmittedReview_GetActiveError_Fails(t *testing.T) {
	store, behavior, messenger := setupReviewFixture(t)
	reviews := &fakeReviewSessions{activeErr: errInjected}
	h := application.NewApproveHandler(store, behavior, messenger, discardLogger(), reviews)

	if err := h.Handle(context.Background(), submittedReviewEvent()); err == nil {
		t.Fatal("a non-NotFound GetActive error should surface, not be swallowed")
	}
}

// Shared: when no message is stored, the reaction handlers are no-ops.
func TestReviewHandlers_NoStoredMessageIsNoop(t *testing.T) {
	behavior := reviewBehavior(false, "")
	cases := []struct {
		name string
		e    kernel.Event
	}{
		{
			name: "approve",
			e:    kernel.Event{Kind: kernel.KindApproved, Repository: "octo/widget", PR: kernel.PR{Number: 42}},
		},
		{
			name: "commented",
			e:    kernel.Event{Kind: kernel.KindReviewCommented, Repository: "octo/widget", PR: kernel.PR{Number: 42}},
		},
		{
			name: "request_change",
			e:    kernel.Event{Kind: kernel.KindChangesRequested, Repository: "octo/widget", PR: kernel.PR{Number: 42}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := newFakeMessageStore() // empty
			messenger := &fakeMessenger{}
			var h domain.Handler
			switch c.name {
			case "approve":
				h = application.NewApproveHandler(store, behavior, messenger, discardLogger(), noActiveSession())
			case "commented":
				h = application.NewCommentedHandler(store, behavior, messenger, discardLogger(), noActiveSession())
			case "request_change":
				h = application.NewRequestChangeHandler(store, behavior, messenger, discardLogger(), noActiveSession())
			}
			if err := h.Handle(context.Background(), c.e); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if len(messenger.reactions) != 0 {
				t.Errorf("AddReaction called when no message stored: %v", messenger.reactionEmojis())
			}
		})
	}
}
