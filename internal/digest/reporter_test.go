package digest

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func sectionTextOf(m slack.Message) string {
	for _, b := range m.Blocks {
		if b.Type == "section" && b.Text != nil {
			return b.Text.Text
		}
	}
	return ""
}

type fakeFinder struct{ rows []store.SlackMessage }

func (f fakeFinder) FindStuck(context.Context, time.Time) ([]store.SlackMessage, error) {
	return f.rows, nil
}

type fakeMappings struct{ byRepo map[string]store.RepoMapping }

func (f fakeMappings) Get(_ context.Context, repo string) (store.RepoMapping, error) {
	m, ok := f.byRepo[repo]
	if !ok {
		return store.RepoMapping{}, store.ErrNotFound
	}
	return m, nil
}

type postCall struct {
	channel string
	msg     slack.Message
}

type fakePoster struct{ calls []postCall }

func (f *fakePoster) PostMessage(_ context.Context, channel string, msg slack.Message) (string, error) {
	f.calls = append(f.calls, postCall{channel, msg})
	return "ts", nil
}

func TestReporter_Report_PostsOneDigestPerChannel(t *testing.T) {
	now := time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local)
	twoDaysAgo := time.Date(2026, 6, 6, 12, 0, 0, 0, time.Local)

	finder := fakeFinder{rows: []store.SlackMessage{
		{PRNumber: 42, Repository: "acme/api", TS: "t1", UpdatedAt: twoDaysAgo},
		{PRNumber: 51, Repository: "acme/web", TS: "t2", UpdatedAt: twoDaysAgo},
		{PRNumber: 88, Repository: "beta/x", TS: "t3", UpdatedAt: twoDaysAgo},
		{PRNumber: 99, Repository: "ghost/unmapped", TS: "t4", UpdatedAt: twoDaysAgo},
	}}
	mappings := fakeMappings{byRepo: map[string]store.RepoMapping{
		"acme/api": {Repository: "acme/api", SlackChannel: "C_ACME", Mentions: []string{"<!channel>"}},
		"acme/web": {Repository: "acme/web", SlackChannel: "C_ACME", Mentions: []string{"<!channel>"}},
		"beta/x":   {Repository: "beta/x", SlackChannel: "C_BETA", Mentions: []string{"<@U1>"}},
	}}
	poster := &fakePoster{}

	r := NewReporter(finder, mappings, poster, slack.NewComposer("eyes"), discardLogger())
	r.now = func() time.Time { return now }

	if err := r.Report(context.Background()); err != nil {
		t.Fatalf("Report: %v", err)
	}

	if len(poster.calls) != 2 {
		t.Fatalf("posted %d digests; want 2 (one per channel, unmapped repo skipped)", len(poster.calls))
	}
	// First-seen channel order: C_ACME (acme/api) then C_BETA.
	if poster.calls[0].channel != "C_ACME" || poster.calls[1].channel != "C_BETA" {
		t.Fatalf("channels = %q, %q; want C_ACME, C_BETA", poster.calls[0].channel, poster.calls[1].channel)
	}

	acme := sectionTextOf(poster.calls[0].msg)
	for _, want := range []string{
		"<!channel>,",
		"2 open PRs waiting for review",
		"<https://github.com/acme/api/pull/42|acme/api #42> · idle 2 days",
		"<https://github.com/acme/web/pull/51|acme/web #51> · idle 2 days",
	} {
		if !strings.Contains(acme, want) {
			t.Errorf("acme digest missing %q\ngot: %s", want, acme)
		}
	}
	if strings.Contains(acme, "ghost/unmapped") {
		t.Errorf("unmapped repo leaked into a digest: %s", acme)
	}
}

func TestReporter_Report_NoStuckPRsPostsNothing(t *testing.T) {
	poster := &fakePoster{}
	r := NewReporter(fakeFinder{}, fakeMappings{}, poster, slack.NewComposer("eyes"), discardLogger())

	if err := r.Report(context.Background()); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(poster.calls) != 0 {
		t.Fatalf("posted %d digests on an empty result; want 0", len(poster.calls))
	}
}

func TestReporter_IdleDays(t *testing.T) {
	now := time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local)
	cases := []struct {
		updated time.Time
		want    int
	}{
		{time.Date(2026, 6, 7, 23, 0, 0, 0, time.Local), 1},
		{time.Date(2026, 6, 5, 1, 0, 0, 0, time.Local), 3},
		{time.Date(2026, 6, 8, 0, 0, 0, 0, time.Local), 1}, // floored at 1
	}
	for _, c := range cases {
		if got := idleDays(now, c.updated); got != c.want {
			t.Errorf("idleDays(%v) = %d; want %d", c.updated, got, c.want)
		}
	}
}
