package pullrequest_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/mptooling/notifycat/internal/aireview"
	"github.com/mptooling/notifycat/internal/pullrequest"
	"github.com/mptooling/notifycat/internal/store"
)

func testDetector() *aireview.Detector { return aireview.NewDetector() }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// reviewBehavior returns a fakeBehavior for octo/widget with the standard
// review reactions and the given IgnoreAIReviews / BotReview settings.
func reviewBehavior(ignoreAI bool, botReview string) *fakeBehavior {
	return &fakeBehavior{m: store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: ignoreAI,
		Reactions: store.Reactions{
			Approved:      "white_check_mark",
			Commented:     "speech_balloon",
			RequestChange: "exclamation",
			BotReview:     botReview,
		},
	}}
}

// setupReviewFixture seeds one stored message (channel C123) for octo/widget#42
// and returns the store, a default behavior, and a fresh messenger.
func setupReviewFixture(t *testing.T) (*fakePRStore, *fakeBehavior, *fakeMessenger) {
	t.Helper()
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	return prStore, reviewBehavior(false, ""), &fakeMessenger{}
}

// ----- Approve -----

func TestApproveHandler_Applicable(t *testing.T) {
	h := pullrequest.NewApproveHandler(nil, nil, nil, discardLogger(), testDetector(), newFakeReviewSessions())

	if !h.Applicable(pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "approved"}}) {
		t.Error("submitted+approved should be applicable")
	}
	if h.Applicable(pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "commented"}}) {
		t.Error("submitted+commented should not be approve-applicable")
	}
	if h.Applicable(pullrequest.Event{Action: "submitted"}) {
		t.Error("submitted with no review should not be applicable")
	}
}

func TestApproveHandler_Handle_AddsReaction(t *testing.T) {
	prStore, behavior, client := setupReviewFixture(t)
	h := pullrequest.NewApproveHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action:     "submitted",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42},
		Review:     &pullrequest.Review{State: "approved"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 1 || client.calls[0].Method != "AddReaction" {
		t.Fatalf("calls = %v", client.methods())
	}
	if client.calls[0].Name != "white_check_mark" {
		t.Errorf("reaction name = %q", client.calls[0].Name)
	}
	if client.calls[0].Channel != "C123" || client.calls[0].TS != "ts1" {
		t.Errorf("reaction target = (%q, %q); want (C123, ts1)", client.calls[0].Channel, client.calls[0].TS)
	}
}

func TestApproveHandler_Handle_TouchesActivity(t *testing.T) {
	prStore, behavior, client := setupReviewFixture(t)
	h := pullrequest.NewApproveHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action:     "submitted",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42},
		Review:     &pullrequest.Review{State: "approved"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if prStore.touchedCount("octo/widget", 42) != 1 {
		t.Fatalf("review activity not recorded via Touch: %d", prStore.touchedCount("octo/widget", 42))
	}
}

func TestApproveHandler_IgnoreAIReviews_BotSenderDoesNotTouch(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(true, "")
	client := &fakeMessenger{}
	h := pullrequest.NewApproveHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR:     pullrequest.PR{Number: 42},
		Review: &pullrequest.Review{State: "approved"},
		Sender: pullrequest.Sender{Login: "copilot[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if prStore.touchedCount("octo/widget", 42) != 0 {
		t.Fatalf("suppressed AI review reset the idle clock via Touch: %d", prStore.touchedCount("octo/widget", 42))
	}
	if len(client.calls) != 0 {
		t.Fatalf("suppressed AI review should not call Slack: %v", client.methods())
	}
}

// ----- Commented -----

func TestCommentedHandler_Applicable(t *testing.T) {
	h := pullrequest.NewCommentedHandler(nil, nil, nil, discardLogger(), testDetector(), newFakeReviewSessions())

	cases := []struct {
		name string
		e    pullrequest.Event
		want bool
	}{
		{"submitted+commented", pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "commented"}}, true},
		{"edited+commented", pullrequest.Event{Action: "edited", Review: &pullrequest.Review{State: "commented"}}, true},
		{"line comment created", pullrequest.Event{GitHubEvent: "pull_request_review_comment", Action: "created"}, true},
		{"line comment edited", pullrequest.Event{GitHubEvent: "pull_request_review_comment", Action: "edited"}, false},
		{"pr conversation comment created", pullrequest.Event{GitHubEvent: "issue_comment", Action: "created", PRComment: true}, true},
		{"pr conversation comment edited", pullrequest.Event{GitHubEvent: "issue_comment", Action: "edited", PRComment: true}, false},
		{"plain issue comment created", pullrequest.Event{GitHubEvent: "issue_comment", Action: "created", PRComment: false}, false},
		{"submitted+approved", pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "approved"}}, false},
		{"submitted no review", pullrequest.Event{Action: "submitted"}, false},
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
	prStore, behavior, client := setupReviewFixture(t)
	h := pullrequest.NewCommentedHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR: pullrequest.PR{Number: 42}, Review: &pullrequest.Review{State: "commented"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 1 || client.calls[0].Name != "speech_balloon" {
		t.Fatalf("calls = %+v", client.calls)
	}
}

func TestCommentedHandler_Handle_LineCommentAddsReaction(t *testing.T) {
	prStore, behavior, client := setupReviewFixture(t)
	h := pullrequest.NewCommentedHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		GitHubEvent: "pull_request_review_comment",
		Action:      "created",
		Repository:  "octo/widget",
		PR:          pullrequest.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 1 || client.calls[0].Name != "speech_balloon" {
		t.Fatalf("calls = %+v", client.calls)
	}
}

// ----- RequestChange -----

func TestRequestChangeHandler_Applicable(t *testing.T) {
	h := pullrequest.NewRequestChangeHandler(nil, nil, nil, discardLogger(), testDetector(), newFakeReviewSessions())

	if !h.Applicable(pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "changes_requested"}}) {
		t.Error("submitted+changes_requested should be applicable")
	}
	if h.Applicable(pullrequest.Event{Action: "edited", Review: &pullrequest.Review{State: "changes_requested"}}) {
		t.Error("edited+changes_requested should not be applicable (PHP parity)")
	}
}

func TestRequestChangeHandler_Handle_AddsReaction(t *testing.T) {
	prStore, behavior, client := setupReviewFixture(t)
	h := pullrequest.NewRequestChangeHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR: pullrequest.PR{Number: 42}, Review: &pullrequest.Review{State: "changes_requested"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 1 || client.calls[0].Name != "exclamation" {
		t.Fatalf("calls = %+v", client.calls)
	}
}

// ----- Fan-out: react on every stored message -----

func TestReactionHandler_ReactsOnEveryMessage(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C0A", "ts-a")
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C0B", "ts-b")
	behavior := reviewBehavior(false, "")
	client := &fakeMessenger{}
	h := pullrequest.NewApproveHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR: pullrequest.PR{Number: 42}, Review: &pullrequest.Review{State: "approved"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if client.reactions() != 2 {
		t.Fatalf("want one reaction per stored message (2); got %d", client.reactions())
	}
	if prStore.touchedCount("octo/widget", 42) != 1 {
		t.Fatalf("want exactly one Touch; got %d", prStore.touchedCount("octo/widget", 42))
	}
}

// ----- Bot-reviewer suppression -----

func TestApproveHandler_IgnoreAIReviews_BotSenderSuppressesReaction(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(true, "")
	client := &fakeMessenger{}
	h := pullrequest.NewApproveHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR:     pullrequest.PR{Number: 42},
		Review: &pullrequest.Review{State: "approved"},
		Sender: pullrequest.Sender{Login: "copilot[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("Slack called for bot reviewer when IgnoreAIReviews=true: %v", client.methods())
	}
}

func TestApproveHandler_IgnoreAIReviews_HumanSenderReacts(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(true, "")
	client := &fakeMessenger{}
	h := pullrequest.NewApproveHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR:     pullrequest.PR{Number: 42},
		Review: &pullrequest.Review{State: "approved"},
		Sender: pullrequest.Sender{Login: "alice", Type: "User"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("human reviewer was incorrectly suppressed: %v", client.methods())
	}
}

func TestApproveHandler_IgnoreAIReviewsFalse_BotSenderStillReacts(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(false, "")
	client := &fakeMessenger{}
	h := pullrequest.NewApproveHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR:     pullrequest.PR{Number: 42},
		Review: &pullrequest.Review{State: "approved"},
		Sender: pullrequest.Sender{Login: "dependabot[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("IgnoreAIReviews=false should allow bot reviewer: %v", client.methods())
	}
}

func TestCommentedHandler_IgnoreAIReviews_BotSenderSuppressesReaction(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(true, "")
	client := &fakeMessenger{}
	h := pullrequest.NewCommentedHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR:     pullrequest.PR{Number: 42},
		Review: &pullrequest.Review{State: "commented"},
		Sender: pullrequest.Sender{Login: "claude[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("Slack called for bot commenter: %v", client.methods())
	}
}

func TestCommentedHandler_IgnoreAIReviews_BotLineCommentSuppressed(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(true, "")
	client := &fakeMessenger{}
	h := pullrequest.NewCommentedHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		GitHubEvent: "pull_request_review_comment",
		Action:      "created",
		Repository:  "octo/widget",
		PR:          pullrequest.PR{Number: 42},
		Sender:      pullrequest.Sender{Login: "github-actions[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("Slack called for bot line-commenter: %v", client.methods())
	}
}

func TestRequestChangeHandler_IgnoreAIReviews_BotSenderSuppressesReaction(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(true, "")
	client := &fakeMessenger{}
	h := pullrequest.NewRequestChangeHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR:     pullrequest.PR{Number: 42},
		Review: &pullrequest.Review{State: "changes_requested"},
		Sender: pullrequest.Sender{Login: "release-please[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("Slack called for bot reviewer requesting changes: %v", client.methods())
	}
}

func TestReactionHandler_SuppressedReactionLogsAtDebug(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(true, "")
	client := &fakeMessenger{}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := pullrequest.NewApproveHandler(prStore, behavior, client, logger, testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR:     pullrequest.PR{Number: 42},
		Review: &pullrequest.Review{State: "approved"},
		Sender: pullrequest.Sender{Login: "copilot[bot]", Type: "Bot"},
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
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(false, "robot_face")
	client := &fakeMessenger{}
	h := pullrequest.NewCommentedHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR:     pullrequest.PR{Number: 42},
		Review: &pullrequest.Review{State: "commented"},
		Sender: pullrequest.Sender{Login: "copilot[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("want state reaction + bot marker; got calls = %+v", client.calls)
	}
	if client.calls[0].Name != "speech_balloon" || client.calls[1].Name != "robot_face" {
		t.Errorf("reactions = [%q, %q]; want [speech_balloon, robot_face]", client.calls[0].Name, client.calls[1].Name)
	}
}

func TestApproveHandler_BotMarker_AddsMarkerAlongsideStateReaction(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(false, "robot_face")
	client := &fakeMessenger{}
	h := pullrequest.NewApproveHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR:     pullrequest.PR{Number: 42},
		Review: &pullrequest.Review{State: "approved"},
		Sender: pullrequest.Sender{Login: "dependabot[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 2 || client.calls[0].Name != "white_check_mark" || client.calls[1].Name != "robot_face" {
		t.Fatalf("want [white_check_mark, robot_face]; got %+v", client.calls)
	}
}

func TestCommentedHandler_BotMarker_LineCommentBotGetsMarker(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(false, "robot_face")
	client := &fakeMessenger{}
	h := pullrequest.NewCommentedHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		GitHubEvent: "pull_request_review_comment",
		Action:      "created",
		Repository:  "octo/widget",
		PR:          pullrequest.PR{Number: 42},
		Sender:      pullrequest.Sender{Login: "github-actions[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 2 || client.calls[1].Name != "robot_face" {
		t.Fatalf("line-comment bot should also get the marker; got %+v", client.calls)
	}
}

func TestCommentedHandler_BotMarker_HumanGetsOnlyStateReaction(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(false, "robot_face")
	client := &fakeMessenger{}
	h := pullrequest.NewCommentedHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR:     pullrequest.PR{Number: 42},
		Review: &pullrequest.Review{State: "commented"},
		Sender: pullrequest.Sender{Login: "alice", Type: "User"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 1 || client.calls[0].Name != "speech_balloon" {
		t.Fatalf("human reviewer should get only the state reaction; got %+v", client.calls)
	}
}

// Suppression wins over the marker: an ignored bot gets no reaction at all,
// not even the distinct marker.
func TestCommentedHandler_BotMarker_SuppressedBotGetsNothing(t *testing.T) {
	prStore := newFakePRStore()
	_ = prStore.AddMessage(context.Background(), "octo/widget", 42, "C123", "ts1")
	behavior := reviewBehavior(true, "robot_face")
	client := &fakeMessenger{}
	h := pullrequest.NewCommentedHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR:     pullrequest.PR{Number: 42},
		Review: &pullrequest.Review{State: "commented"},
		Sender: pullrequest.Sender{Login: "copilot[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("ignored bot should get no reaction even with a marker set; got %+v", client.calls)
	}
}

// ----- Finish-on-submit -----

func TestApproveHandler_SubmittedReview_FinishesSession(t *testing.T) {
	prStore, behavior, client := setupReviewFixture(t)
	reviews := newFakeReviewSessions()
	h := pullrequest.NewApproveHandler(prStore, behavior, client, discardLogger(), testDetector(), reviews)

	e := pullrequest.Event{
		GitHubEvent: "pull_request_review",
		Action:      "submitted",
		Repository:  "octo/widget",
		PR:          pullrequest.PR{Number: 42},
		Review:      &pullrequest.Review{State: "approved"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.finishedCount("octo/widget", 42) != 1 {
		t.Fatalf("approved review should finish session; got finishedCount=%d", reviews.finishedCount("octo/widget", 42))
	}
	if prStore.touchedCount("octo/widget", 42) != 1 {
		t.Fatalf("Touch should still have been called; got %d", prStore.touchedCount("octo/widget", 42))
	}
	if client.reactions() != 1 {
		t.Fatalf("reaction should still have been added; got %d", client.reactions())
	}
}

func TestRequestChangeHandler_SubmittedReview_FinishesSession(t *testing.T) {
	prStore, behavior, client := setupReviewFixture(t)
	reviews := newFakeReviewSessions()
	h := pullrequest.NewRequestChangeHandler(prStore, behavior, client, discardLogger(), testDetector(), reviews)

	e := pullrequest.Event{
		GitHubEvent: "pull_request_review",
		Action:      "submitted",
		Repository:  "octo/widget",
		PR:          pullrequest.PR{Number: 42},
		Review:      &pullrequest.Review{State: "changes_requested"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.finishedCount("octo/widget", 42) != 1 {
		t.Fatalf("request-change review should finish session; got finishedCount=%d", reviews.finishedCount("octo/widget", 42))
	}
}

func TestCommentedHandler_LineComment_DoesNotFinishSession(t *testing.T) {
	prStore, behavior, client := setupReviewFixture(t)
	reviews := newFakeReviewSessions()
	h := pullrequest.NewCommentedHandler(prStore, behavior, client, discardLogger(), testDetector(), reviews)

	e := pullrequest.Event{
		GitHubEvent: "pull_request_review_comment",
		Action:      "created",
		Repository:  "octo/widget",
		PR:          pullrequest.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.finishedCount("octo/widget", 42) != 0 {
		t.Fatalf("line comment should not finish session; got finishedCount=%d", reviews.finishedCount("octo/widget", 42))
	}
}

func TestCommentedHandler_IssueComment_DoesNotFinishSession(t *testing.T) {
	prStore, behavior, client := setupReviewFixture(t)
	reviews := newFakeReviewSessions()
	h := pullrequest.NewCommentedHandler(prStore, behavior, client, discardLogger(), testDetector(), reviews)

	e := pullrequest.Event{
		GitHubEvent: "issue_comment",
		Action:      "created",
		Repository:  "octo/widget",
		PR:          pullrequest.PR{Number: 42},
		PRComment:   true,
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.finishedCount("octo/widget", 42) != 0 {
		t.Fatalf("issue comment should not finish session; got finishedCount=%d", reviews.finishedCount("octo/widget", 42))
	}
}

func TestCommentedHandler_SubmittedCommentReview_FinishesSession(t *testing.T) {
	prStore, behavior, client := setupReviewFixture(t)
	reviews := newFakeReviewSessions()
	h := pullrequest.NewCommentedHandler(prStore, behavior, client, discardLogger(), testDetector(), reviews)

	e := pullrequest.Event{
		GitHubEvent: "pull_request_review",
		Action:      "submitted",
		Repository:  "octo/widget",
		PR:          pullrequest.PR{Number: 42},
		Review:      &pullrequest.Review{State: "commented"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reviews.finishedCount("octo/widget", 42) != 1 {
		t.Fatalf("submitted commented review should finish session; got finishedCount=%d", reviews.finishedCount("octo/widget", 42))
	}
}

// Shared: when no message is stored, the reaction handlers are no-ops.
func TestReviewHandlers_NoStoredMessageIsNoop(t *testing.T) {
	behavior := reviewBehavior(false, "")
	cases := []struct {
		name string
		e    pullrequest.Event
	}{
		{
			name: "approve",
			e:    pullrequest.Event{Action: "submitted", Repository: "octo/widget", PR: pullrequest.PR{Number: 42}, Review: &pullrequest.Review{State: "approved"}},
		},
		{
			name: "commented",
			e:    pullrequest.Event{Action: "submitted", Repository: "octo/widget", PR: pullrequest.PR{Number: 42}, Review: &pullrequest.Review{State: "commented"}},
		},
		{
			name: "request_change",
			e:    pullrequest.Event{Action: "submitted", Repository: "octo/widget", PR: pullrequest.PR{Number: 42}, Review: &pullrequest.Review{State: "changes_requested"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prStore := newFakePRStore() // empty
			client := &fakeMessenger{}
			var h pullrequest.EventHandler
			switch c.name {
			case "approve":
				h = pullrequest.NewApproveHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())
			case "commented":
				h = pullrequest.NewCommentedHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())
			case "request_change":
				h = pullrequest.NewRequestChangeHandler(prStore, behavior, client, discardLogger(), testDetector(), newFakeReviewSessions())
			}
			if err := h.Handle(context.Background(), c.e); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if len(client.calls) != 0 {
				t.Errorf("Slack called when no message stored: %v", client.methods())
			}
		})
	}
}
