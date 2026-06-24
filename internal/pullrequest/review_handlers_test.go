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

func setupReviewFixture(t *testing.T) (*fakeSlackMessages, *fakeRepoMappings, *fakeSlackClient) {
	t.Helper()
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: false,
		Reactions: store.Reactions{
			Approved:      "white_check_mark",
			Commented:     "speech_balloon",
			RequestChange: "exclamation",
		},
	})
	return msgs, mappings, &fakeSlackClient{}
}

// ----- Approve -----

func TestApproveHandler_Applicable(t *testing.T) {
	h := pullrequest.NewApproveHandler(nil, nil, nil, discardLogger(), testDetector())

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
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), testDetector())

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
}

func TestApproveHandler_Handle_TouchesActivity(t *testing.T) {
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), testDetector())

	e := pullrequest.Event{
		Action:     "submitted",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42},
		Review:     &pullrequest.Review{State: "approved"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(msgs.touched) != 1 || msgs.touched[0] != (fakeKey{"octo/widget", 42}) {
		t.Fatalf("review activity not recorded via Touch: %v", msgs.touched)
	}
}

func TestApproveHandler_IgnoreAIReviews_BotSenderDoesNotTouch(t *testing.T) {
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: true,
		Reactions: store.Reactions{
			Approved: "white_check_mark",
		},
	})
	client := &fakeSlackClient{}
	h := pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), testDetector())

	e := pullrequest.Event{
		Action: "submitted", Repository: "octo/widget",
		PR:     pullrequest.PR{Number: 42},
		Review: &pullrequest.Review{State: "approved"},
		Sender: pullrequest.Sender{Login: "copilot[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(msgs.touched) != 0 {
		t.Fatalf("suppressed AI review reset the idle clock via Touch: %v", msgs.touched)
	}
	if len(client.calls) != 0 {
		t.Fatalf("suppressed AI review should not call Slack: %v", client.methods())
	}
}

// ----- Commented -----

func TestCommentedHandler_Applicable(t *testing.T) {
	h := pullrequest.NewCommentedHandler(nil, nil, nil, discardLogger(), testDetector())

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
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), testDetector())

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
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), testDetector())

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
	h := pullrequest.NewRequestChangeHandler(nil, nil, nil, discardLogger(), testDetector())

	if !h.Applicable(pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "changes_requested"}}) {
		t.Error("submitted+changes_requested should be applicable")
	}
	if h.Applicable(pullrequest.Event{Action: "edited", Review: &pullrequest.Review{State: "changes_requested"}}) {
		t.Error("edited+changes_requested should not be applicable (PHP parity)")
	}
}

func TestRequestChangeHandler_Handle_AddsReaction(t *testing.T) {
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewRequestChangeHandler(msgs, mappings, client, discardLogger(), testDetector())

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

// ----- Bot-reviewer suppression -----

func TestApproveHandler_IgnoreAIReviews_BotSenderSuppressesReaction(t *testing.T) {
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: true,
		Reactions: store.Reactions{
			Approved: "white_check_mark",
		},
	})
	client := &fakeSlackClient{}
	h := pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), testDetector())

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
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: true,
		Reactions: store.Reactions{
			Approved: "white_check_mark",
		},
	})
	client := &fakeSlackClient{}
	h := pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), testDetector())

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
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: false,
		Reactions: store.Reactions{
			Approved: "white_check_mark",
		},
	})
	client := &fakeSlackClient{}
	h := pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), testDetector())

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
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: true,
		Reactions: store.Reactions{
			Commented: "speech_balloon",
		},
	})
	client := &fakeSlackClient{}
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), testDetector())

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
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: true,
		Reactions: store.Reactions{
			Commented: "speech_balloon",
		},
	})
	client := &fakeSlackClient{}
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), testDetector())

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
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: true,
		Reactions: store.Reactions{
			RequestChange: "exclamation",
		},
	})
	client := &fakeSlackClient{}
	h := pullrequest.NewRequestChangeHandler(msgs, mappings, client, discardLogger(), testDetector())

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
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: true,
		Reactions: store.Reactions{
			Approved: "white_check_mark",
		},
	})
	client := &fakeSlackClient{}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := pullrequest.NewApproveHandler(msgs, mappings, client, logger, testDetector())

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
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: false,
		Reactions: store.Reactions{
			Commented: "speech_balloon",
			BotReview: "robot_face",
		},
	})
	client := &fakeSlackClient{}
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), testDetector())

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
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: false,
		Reactions: store.Reactions{
			Approved:  "white_check_mark",
			BotReview: "robot_face",
		},
	})
	client := &fakeSlackClient{}
	h := pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), testDetector())

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
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: false,
		Reactions: store.Reactions{
			Commented: "speech_balloon",
			BotReview: "robot_face",
		},
	})
	client := &fakeSlackClient{}
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), testDetector())

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
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: false,
		Reactions: store.Reactions{
			Commented: "speech_balloon",
			BotReview: "robot_face",
		},
	})
	client := &fakeSlackClient{}
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), testDetector())

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
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "ts1",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: true,
		Reactions: store.Reactions{
			Commented: "speech_balloon",
			BotReview: "robot_face",
		},
	})
	client := &fakeSlackClient{}
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), testDetector())

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

// Shared: when no SlackMessage exists, the reaction handlers are no-ops.
func TestReviewHandlers_NoStoredMessageIsNoop(t *testing.T) {
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository:      "octo/widget",
		SlackChannel:    "C123",
		IgnoreAIReviews: false,
		Reactions: store.Reactions{
			Approved:      "x",
			Commented:     "x",
			RequestChange: "x",
		},
	})
	type ctor func() pullrequest.EventHandler
	cases := []struct {
		name string
		ctor ctor
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
			msgs := newFakeSlackMessages() // empty
			client := &fakeSlackClient{}
			var h pullrequest.EventHandler
			switch c.name {
			case "approve":
				h = pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), testDetector())
			case "commented":
				h = pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), testDetector())
			case "request_change":
				h = pullrequest.NewRequestChangeHandler(msgs, mappings, client, discardLogger(), testDetector())
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
