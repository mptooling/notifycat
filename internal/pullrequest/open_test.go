package pullrequest_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/pullrequest"
	"github.com/mptooling/notifycat/internal/slack"
	storepkg "github.com/mptooling/notifycat/internal/store"
)

func testComposer() *slack.Composer { return slack.NewComposer("rocket") }

func newOpenHandler(
	t *testing.T,
	st *fakePRStore,
	resolver *fakeTargetResolver,
	client *fakeMessenger,
) *pullrequest.OpenHandler {
	t.Helper()
	return pullrequest.NewOpenHandler(
		st, resolver, client,
		testComposer(),
		slog.New(slog.NewTextHandler(devNull{}, nil)),
	)
}

// devNull satisfies io.Writer for the discard logger in this file.
type devNull struct{}

func (devNull) Write(p []byte) (int, error) { return len(p), nil }

func openedEvent(repo string, prNumber int) pullrequest.Event {
	return pullrequest.Event{
		Action:     "opened",
		Repository: repo,
		PR:         pullrequest.PR{Number: prNumber, Title: fmt.Sprintf("PR #%d", prNumber), Draft: false},
	}
}

func TestOpenHandler_Applicable(t *testing.T) {
	h := newOpenHandler(t, newFakePRStore(), &fakeTargetResolver{}, &fakeMessenger{})

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
	st := newFakePRStore()
	resolver := &fakeTargetResolver{
		behavior: storepkg.RepoMapping{
			DependabotFormat: true,
			Reactions:        storepkg.Reactions{NewPR: "rocket"},
		},
		targets: []storepkg.Target{
			{Channel: "C123", Mentions: []string{"@alice"}},
		},
	}
	client := &fakeMessenger{}
	h := newOpenHandler(t, st, resolver, client)

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

	msgs, err := st.Messages(context.Background(), "octo/widget", 42)
	if err != nil {
		t.Fatalf("Messages after Handle: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Channel != "C123" || msgs[0].MessageID == "" {
		t.Errorf("unexpected stored messages: %+v", msgs)
	}
}

func TestOpenHandler_Handle_ThreadsCreatedAtAndFallback(t *testing.T) {
	st := newFakePRStore()
	resolver := &fakeTargetResolver{
		behavior: storepkg.RepoMapping{
			DependabotFormat: true,
			Reactions:        storepkg.Reactions{NewPR: "rocket"},
		},
		targets: []storepkg.Target{
			{Channel: "C123", Mentions: []string{"@alice"}},
		},
	}
	client := &fakeMessenger{}
	h := newOpenHandler(t, st, resolver, client)

	created := time.Date(2026, 6, 5, 14, 4, 0, 0, time.UTC)
	e := pullrequest.Event{
		Action:     "opened",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42, Title: "fix", URL: "u", Author: "alice", CreatedAt: created},
	}
	if err := h.Handle(context.Background(), e); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	msg := client.calls[0].Msg
	ctx := contextTextOf(msg)
	wantToken := fmt.Sprintf("<!date^%d^", created.Unix())
	if !strings.Contains(ctx, "octo/widget · alice · opened ") || !strings.Contains(ctx, wantToken) {
		t.Errorf("context line did not thread repo/author/created time: %q", ctx)
	}
	if msg.Fallback == "" {
		t.Error("posted message has no top-level text fallback")
	}
}

func TestOpenHandler_Handle_SkipsIfMessageAlreadyExists(t *testing.T) {
	st := newFakePRStore()
	// Pre-seed a message for channel C123 so the handler should skip it.
	_ = st.AddMessage(context.Background(), "octo/widget", 42, "C123", "preexisting-ts")

	resolver := &fakeTargetResolver{
		behavior: storepkg.RepoMapping{
			DependabotFormat: true,
			Reactions:        storepkg.Reactions{NewPR: "rocket"},
		},
		targets: []storepkg.Target{
			{Channel: "C123"},
		},
	}
	client := &fakeMessenger{}
	h := newOpenHandler(t, st, resolver, client)

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
	st := newFakePRStore()
	resolver := &fakeTargetResolver{err: storepkg.ErrNotFound}
	client := &fakeMessenger{}
	h := newOpenHandler(t, st, resolver, client)

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
	st := newFakePRStore()
	resolver := &fakeTargetResolver{
		behavior: storepkg.RepoMapping{
			DependabotFormat: true,
			Reactions:        storepkg.Reactions{NewPR: "rocket"},
		},
		targets: []storepkg.Target{
			{Channel: "C123", Mentions: []string{"@alice"}},
		},
	}
	client := &fakeMessenger{}
	h := newOpenHandler(t, st, resolver, client)

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
	st := newFakePRStore()
	resolver := &fakeTargetResolver{
		behavior: storepkg.RepoMapping{
			DependabotFormat: true,
			Reactions:        storepkg.Reactions{NewPR: "rocket"},
		},
		targets: []storepkg.Target{
			{Channel: "C123"},
		},
	}
	client := &fakeMessenger{}
	h := newOpenHandler(t, st, resolver, client)

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
	st := newFakePRStore()
	resolver := &fakeTargetResolver{
		behavior: storepkg.RepoMapping{
			DependabotFormat: true,
			Reactions:        storepkg.Reactions{NewPR: "rocket"},
		},
		targets: []storepkg.Target{
			{Channel: "C123"},
		},
	}
	client := &fakeMessenger{}
	h := newOpenHandler(t, st, resolver, client)

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
	st := newFakePRStore()
	resolver := &fakeTargetResolver{
		behavior: storepkg.RepoMapping{
			DependabotFormat: true,
			Reactions:        storepkg.Reactions{NewPR: "rocket"},
		},
		targets: []storepkg.Target{
			{Channel: "C123"},
		},
	}
	client := &fakeMessenger{}
	h := newOpenHandler(t, st, resolver, client)

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
	st := newFakePRStore()
	resolver := &fakeTargetResolver{
		behavior: storepkg.RepoMapping{
			DependabotFormat: false,
		},
		targets: []storepkg.Target{
			{Channel: "C123"},
		},
	}
	client := &fakeMessenger{}
	h := newOpenHandler(t, st, resolver, client)

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
	st := newFakePRStore()
	resolver := &fakeTargetResolver{
		behavior: storepkg.RepoMapping{
			DependabotFormat: true,
			Reactions:        storepkg.Reactions{NewPR: "rocket"},
		},
		targets: []storepkg.Target{
			{Channel: "C123", Mentions: nil}, // explicitly empty mentions
		},
	}
	client := &fakeMessenger{}
	h := newOpenHandler(t, st, resolver, client)

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
	st := newFakePRStore()
	resolver := &fakeTargetResolver{
		behavior: storepkg.RepoMapping{
			DependabotFormat: true,
			Reactions:        storepkg.Reactions{NewPR: "rocket"},
		},
		targets: []storepkg.Target{
			{Channel: "C123"},
		},
	}
	client := &fakeMessenger{postErr: errInjected}
	h := newOpenHandler(t, st, resolver, client)

	e := pullrequest.Event{
		Action:     "opened",
		Repository: "octo/widget",
		PR:         pullrequest.PR{Number: 42},
	}
	err := h.Handle(context.Background(), e)
	if !errors.Is(err, errInjected) {
		t.Fatalf("Handle error = %v; want errInjected", err)
	}
	if msgs, err := st.Messages(context.Background(), "octo/widget", 42); err == nil {
		t.Errorf("message saved despite Slack failure: %+v", msgs)
	}
}

func TestOpenHandler_FansOutToEachTarget(t *testing.T) {
	st := newFakePRStore()
	resolver := &fakeTargetResolver{
		behavior: storepkg.RepoMapping{Reactions: storepkg.Reactions{NewPR: "eyes"}},
		targets: []storepkg.Target{
			{Channel: "C0A", Mentions: []string{"<@U0A>"}},
			{Channel: "C0B", Mentions: []string{"<@U0B>"}},
		},
	}
	client := &fakeMessenger{}
	h := pullrequest.NewOpenHandler(st, resolver, client, testComposer(), discardLogger())

	if err := h.Handle(context.Background(), openedEvent("acme/web", 7)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if posts := client.postsByChannel(); len(posts) != 2 || posts["C0A"] == 0 || posts["C0B"] == 0 {
		t.Fatalf("want one post per channel; got %+v", posts)
	}
	msgs, _ := st.Messages(context.Background(), "acme/web", 7)
	if len(msgs) != 2 {
		t.Fatalf("want 2 stored messages; got %d", len(msgs))
	}
}
