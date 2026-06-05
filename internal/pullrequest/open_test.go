package pullrequest_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/pullrequest"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

func newOpenHandler(
	t *testing.T,
	msgs *fakeSlackMessages,
	mappings *fakeRepoMappings,
	client *fakeSlackClient,
) *pullrequest.OpenHandler {
	t.Helper()
	return newOpenHandlerWithFormat(t, msgs, mappings, client, true)
}

func newOpenHandlerWithFormat(
	t *testing.T,
	msgs *fakeSlackMessages,
	mappings *fakeRepoMappings,
	client *fakeSlackClient,
	dependabotFormat bool,
) *pullrequest.OpenHandler {
	t.Helper()
	return pullrequest.NewOpenHandler(
		msgs, mappings, client,
		slack.NewComposer("rocket"),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		dependabotFormat,
	)
}

func TestOpenHandler_Applicable(t *testing.T) {
	h := newOpenHandler(t, newFakeSlackMessages(), newFakeRepoMappings(), &fakeSlackClient{})

	cases := []struct {
		name string
		e    pullrequest.Event
		want bool
	}{
		{"opened non-draft", pullrequest.Event{Action: "opened", PR: pullrequest.PR{Draft: false}}, true},
		{"opened draft", pullrequest.Event{Action: "opened", PR: pullrequest.PR{Draft: true}}, false},
		{"ready_for_review", pullrequest.Event{Action: "ready_for_review"}, true},
		{"closed", pullrequest.Event{Action: "closed"}, false},
		{"submitted approved", pullrequest.Event{Action: "submitted", Review: &pullrequest.Review{State: "approved"}}, false},
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
	msgs := newFakeSlackMessages()
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123", Mentions: []string{"@alice"},
	})
	client := &fakeSlackClient{}
	h := newOpenHandler(t, msgs, mappings, client)

	e := pullrequest.Event{
		Action:     "opened",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42, Title: "fix", URL: "u", Author: "a", Draft: false},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(client.calls) != 1 || client.calls[0].Method != "PostMessage" {
		t.Fatalf("calls = %v", client.methods())
	}
	if client.calls[0].Channel != "C123" {
		t.Errorf("channel = %q; want C123", client.calls[0].Channel)
	}
	if !strings.Contains(client.calls[0].Text, "PR #42") {
		t.Errorf("posted text missing PR #42: %q", client.calls[0].Text)
	}

	saved, err := msgs.Get(context.Background(), "octo/widget", 42)
	if err != nil {
		t.Fatalf("Get after Handle: %v", err)
	}
	if saved.TS == "" {
		t.Errorf("saved TS is empty")
	}
}

func TestOpenHandler_Handle_SkipsIfMessageAlreadyExists(t *testing.T) {
	msgs := newFakeSlackMessages()
	_ = msgs.Save(context.Background(), store.SlackMessage{
		PRNumber: 42, Repository: "octo/widget", TS: "preexisting",
	})
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123",
	})
	client := &fakeSlackClient{}
	h := newOpenHandler(t, msgs, mappings, client)

	e := pullrequest.Event{
		Action:     "opened",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 0 {
		t.Errorf("Slack called when message already existed: %v", client.methods())
	}
}

func TestOpenHandler_Handle_SkipsIfNoMapping(t *testing.T) {
	msgs := newFakeSlackMessages()
	mappings := newFakeRepoMappings() // empty
	client := &fakeSlackClient{}
	h := newOpenHandler(t, msgs, mappings, client)

	e := pullrequest.Event{
		Action:     "opened",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(client.calls) != 0 {
		t.Errorf("Slack called when no mapping existed: %v", client.methods())
	}
}

func TestOpenHandler_Handle_DependabotRoutine(t *testing.T) {
	msgs := newFakeSlackMessages()
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123", Mentions: []string{"@alice"},
	})
	client := &fakeSlackClient{}
	h := newOpenHandler(t, msgs, mappings, client)

	e := pullrequest.Event{
		Action:     "opened",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42, Title: "bump acme/lib from 1.2.0 to 1.2.1", URL: "u", Author: "dependabot[bot]"},
		Sender:     pullrequest.Sender{Login: "dependabot[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	text := client.calls[0].Text
	for _, want := range []string{":package:", "dependabot bumped", "bump acme/lib from 1.2.0 to 1.2.1"} {
		if !strings.Contains(text, want) {
			t.Errorf("posted text missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "please review") {
		t.Errorf("dependabot routine message should not say 'please review': %q", text)
	}
}

func TestOpenHandler_Handle_DependabotSecurity(t *testing.T) {
	msgs := newFakeSlackMessages()
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123",
	})
	client := &fakeSlackClient{}
	h := newOpenHandler(t, msgs, mappings, client)

	e := pullrequest.Event{
		Action:     "opened",
		Repository: "octo/widget",
		PR: pullrequest.PR{
			Number: 42, Title: "bump acme/lib from 1.2.0 to 1.2.1", URL: "u", Author: "dependabot[bot]",
			Body: "Bumps acme/lib.\n\n## Vulnerabilities fixed\n\nCVE-2026-1234.",
		},
		Sender: pullrequest.Sender{Login: "dependabot[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	text := client.calls[0].Text
	for _, want := range []string{":rotating_light:", "dependabot security update"} {
		if !strings.Contains(text, want) {
			t.Errorf("posted text missing %q: %q", want, text)
		}
	}
}

func TestOpenHandler_Handle_Renovate(t *testing.T) {
	msgs := newFakeSlackMessages()
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123",
	})
	client := &fakeSlackClient{}
	h := newOpenHandler(t, msgs, mappings, client)

	e := pullrequest.Event{
		Action:     "opened",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 7, Title: "Update acme/lib to v2", URL: "u", Author: "renovate[bot]"},
		Sender:     pullrequest.Sender{Login: "renovate[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	text := client.calls[0].Text
	if !strings.Contains(text, ":package:") || !strings.Contains(text, "renovate bumped") {
		t.Errorf("renovate routine message wrong: %q", text)
	}
}

func TestOpenHandler_Handle_DependabotReadyForReviewByHuman(t *testing.T) {
	// Regression: a draft Dependabot PR marked ready_for_review by a human. The
	// webhook sender is the human who clicked the button, but the PR author is
	// still dependabot[bot] — detection must key off the author so the compact
	// format applies, not off the sender (which would fall back to "please
	// review").
	msgs := newFakeSlackMessages()
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123",
	})
	client := &fakeSlackClient{}
	h := newOpenHandler(t, msgs, mappings, client)

	e := pullrequest.Event{
		Action:     "ready_for_review",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42, Title: "bump acme/lib from 1.2.0 to 1.2.1", URL: "u", Author: "dependabot[bot]"},
		Sender:     pullrequest.Sender{Login: "alice", Type: "User"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	text := client.calls[0].Text
	for _, want := range []string{":package:", "dependabot bumped", "bump acme/lib from 1.2.0 to 1.2.1"} {
		if !strings.Contains(text, want) {
			t.Errorf("posted text missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "please review") {
		t.Errorf("dependabot PR marked ready by a human should still use compact format: %q", text)
	}
}

func TestOpenHandler_Handle_DependabotFormatDisabled(t *testing.T) {
	msgs := newFakeSlackMessages()
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123",
	})
	client := &fakeSlackClient{}
	h := newOpenHandlerWithFormat(t, msgs, mappings, client, false)

	e := pullrequest.Event{
		Action:     "opened",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42, Title: "bump acme/lib", URL: "u", Author: "dependabot[bot]"},
		Sender:     pullrequest.Sender{Login: "dependabot[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	text := client.calls[0].Text
	if !strings.Contains(text, "please review") {
		t.Errorf("with format disabled, dependabot PR should use standard format: %q", text)
	}
	if strings.Contains(text, ":package:") {
		t.Errorf("with format disabled, compact format should not appear: %q", text)
	}
}

func TestOpenHandler_Handle_DependabotEmptyMentions(t *testing.T) {
	msgs := newFakeSlackMessages()
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123",
	})
	client := &fakeSlackClient{}
	h := newOpenHandler(t, msgs, mappings, client)

	e := pullrequest.Event{
		Action:     "opened",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42, Title: "bump acme/lib", URL: "u", Author: "dependabot[bot]"},
		Sender:     pullrequest.Sender{Login: "dependabot[bot]", Type: "Bot"},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	text := client.calls[0].Text
	if strings.Contains(text, ", ,") || strings.Contains(text, ": ,") {
		t.Errorf("stranded comma with empty mentions: %q", text)
	}
}

func TestOpenHandler_Handle_DoesNotPersistOnSlackFailure(t *testing.T) {
	msgs := newFakeSlackMessages()
	mappings := newFakeRepoMappings(store.RepoMapping{
		Repository: "octo/widget", SlackChannel: "C123",
	})
	client := &fakeSlackClient{postErr: errInjected}
	h := newOpenHandler(t, msgs, mappings, client)

	e := pullrequest.Event{
		Action:     "opened",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42},
	}
	err := h.Handle(context.Background(), e)
	if !errors.Is(err, errInjected) {
		t.Fatalf("Handle error = %v; want errInjected", err)
	}
	if _, err := msgs.Get(context.Background(), "octo/widget", 42); err == nil {
		t.Errorf("message saved despite Slack failure")
	}
}
