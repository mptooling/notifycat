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

func disabledDetector() *aireview.Detector { return aireview.NewDetector(false) }
func enabledDetector() *aireview.Detector  { return aireview.NewDetector(true) }

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
		Repository: "octo/widget", SlackChannel: "C123",
	})
	return msgs, mappings, &fakeSlackClient{}
}

// ----- Approve -----

func TestApproveHandler_Applicable(t *testing.T) {
	h := pullrequest.NewApproveHandler(nil, nil, nil, discardLogger(), "white_check_mark", disabledDetector())

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
	h := pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), "white_check_mark", disabledDetector())

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

// ----- Commented -----

func TestCommentedHandler_Applicable(t *testing.T) {
	h := pullrequest.NewCommentedHandler(nil, nil, nil, discardLogger(), "speech_balloon", disabledDetector())

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
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), "speech_balloon", disabledDetector())

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
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), "speech_balloon", disabledDetector())

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
	h := pullrequest.NewRequestChangeHandler(nil, nil, nil, discardLogger(), "exclamation", disabledDetector())

	if !h.Applicable(pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "changes_requested"}}) {
		t.Error("submitted+changes_requested should be applicable")
	}
	if h.Applicable(pullrequest.Event{Action: "edited", Review: &pullrequest.Review{State: "changes_requested"}}) {
		t.Error("edited+changes_requested should not be applicable (PHP parity)")
	}
}

func TestRequestChangeHandler_Handle_AddsReaction(t *testing.T) {
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewRequestChangeHandler(msgs, mappings, client, discardLogger(), "exclamation", disabledDetector())

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

func TestApproveHandler_DetectorEnabled_BotSenderSuppressesReaction(t *testing.T) {
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), "white_check_mark", enabledDetector())

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
		t.Fatalf("Slack called for bot reviewer with detector enabled: %v", client.methods())
	}
}

func TestApproveHandler_DetectorEnabled_HumanSenderReacts(t *testing.T) {
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), "white_check_mark", enabledDetector())

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

func TestApproveHandler_DetectorDisabled_BotSenderStillReacts(t *testing.T) {
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), "white_check_mark", disabledDetector())

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
		t.Fatalf("disabled detector silently suppressed bot reviewer: %v", client.methods())
	}
}

func TestCommentedHandler_DetectorEnabled_BotSenderSuppressesReaction(t *testing.T) {
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), "speech_balloon", enabledDetector())

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

func TestCommentedHandler_DetectorEnabled_BotLineCommentSuppressed(t *testing.T) {
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), "speech_balloon", enabledDetector())

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

func TestRequestChangeHandler_DetectorEnabled_BotSenderSuppressesReaction(t *testing.T) {
	msgs, mappings, client := setupReviewFixture(t)
	h := pullrequest.NewRequestChangeHandler(msgs, mappings, client, discardLogger(), "exclamation", enabledDetector())

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
	msgs, mappings, client := setupReviewFixture(t)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := pullrequest.NewApproveHandler(msgs, mappings, client, logger, "white_check_mark", enabledDetector())

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

// Shared: when no SlackMessage exists, the reaction handlers are no-ops.
func TestReviewHandlers_NoStoredMessageIsNoop(t *testing.T) {
	mappings := newFakeRepoMappings(store.RepoMapping{Repository: "octo/widget", SlackChannel: "C123"})
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
				h = pullrequest.NewApproveHandler(msgs, mappings, client, discardLogger(), "x", disabledDetector())
			case "commented":
				h = pullrequest.NewCommentedHandler(msgs, mappings, client, discardLogger(), "x", disabledDetector())
			case "request_change":
				h = pullrequest.NewRequestChangeHandler(msgs, mappings, client, discardLogger(), "x", disabledDetector())
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
