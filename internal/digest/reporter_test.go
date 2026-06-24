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
	channel  string
	threadTS string // "" for a parent post, the parent ts for a reply
	msg      slack.Message
}

type fakePoster struct{ calls []postCall }

func (f *fakePoster) PostMessage(_ context.Context, channel string, msg slack.Message) (string, error) {
	f.calls = append(f.calls, postCall{channel: channel, msg: msg})
	return "ts-" + channel, nil
}

func (f *fakePoster) PostReply(_ context.Context, channel, threadTS string, msg slack.Message) (string, error) {
	f.calls = append(f.calls, postCall{channel: channel, threadTS: threadTS, msg: msg})
	return "reply-" + channel, nil
}

func TestReporter_Report_PostsParentThenThreadedListPerChannel(t *testing.T) {
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

	// Two channels × (one parent + one threaded list reply).
	if len(poster.calls) != 4 {
		t.Fatalf("posted %d messages; want 4 (parent + list per channel)", len(poster.calls))
	}

	// C_ACME: parent then list reply on the parent's ts.
	parent, list := poster.calls[0], poster.calls[1]
	if parent.channel != "C_ACME" || parent.threadTS != "" {
		t.Fatalf("call[0] = %+v; want a C_ACME parent post", parent)
	}
	if list.channel != "C_ACME" || list.threadTS != "ts-C_ACME" {
		t.Fatalf("call[1] = %+v; want a C_ACME reply threaded on ts-C_ACME", list)
	}

	parentText := sectionTextOf(parent.msg)
	for _, want := range []string{"<!channel>,", "2 open PRs waiting for review"} {
		if !strings.Contains(parentText, want) {
			t.Errorf("acme parent missing %q\ngot: %s", want, parentText)
		}
	}
	if strings.Contains(parentText, "idle") || strings.Contains(parentText, "pull/") {
		t.Errorf("parent must not carry the PR list: %s", parentText)
	}

	listText := sectionTextOf(list.msg)
	for _, want := range []string{
		"<https://github.com/acme/api/pull/42|acme/api #42> · idle 2 days",
		"<https://github.com/acme/web/pull/51|acme/web #51> · idle 2 days",
	} {
		if !strings.Contains(listText, want) {
			t.Errorf("acme list missing %q\ngot: %s", want, listText)
		}
	}
	if strings.Contains(listText, "ghost/unmapped") {
		t.Errorf("unmapped repo leaked into the list: %s", listText)
	}

	// C_BETA follows in first-seen order.
	if poster.calls[2].channel != "C_BETA" || poster.calls[2].threadTS != "" {
		t.Fatalf("call[2] = %+v; want a C_BETA parent post", poster.calls[2])
	}
	if poster.calls[3].threadTS != "ts-C_BETA" {
		t.Fatalf("call[3] = %+v; want a C_BETA reply threaded on ts-C_BETA", poster.calls[3])
	}
}

// Each stuck PR must surface in exactly one channel's list and exactly once.
// The same PR number living in two different repos is two distinct PRs, so each
// belongs only in its own repo's channel — the dedup key is (repo, number),
// never the number alone.
func TestReporter_Report_NoPRDuplicatedAcrossChannels(t *testing.T) {
	now := time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local)
	twoDaysAgo := time.Date(2026, 6, 6, 12, 0, 0, 0, time.Local)

	finder := fakeFinder{rows: []store.SlackMessage{
		{PRNumber: 42, Repository: "acme/api", TS: "t1", UpdatedAt: twoDaysAgo},
		{PRNumber: 42, Repository: "beta/web", TS: "t2", UpdatedAt: twoDaysAgo}, // same number, different repo
	}}
	mappings := fakeMappings{byRepo: map[string]store.RepoMapping{
		"acme/api": {Repository: "acme/api", SlackChannel: "C_ACME"},
		"beta/web": {Repository: "beta/web", SlackChannel: "C_BETA"},
	}}
	poster := &fakePoster{}

	r := NewReporter(finder, mappings, poster, slack.NewComposer("eyes"), discardLogger())
	r.now = func() time.Time { return now }

	if err := r.Report(context.Background()); err != nil {
		t.Fatalf("Report: %v", err)
	}

	// Every list reply, joined, must mention each PR's URL exactly once — and
	// only in the reply for its own channel.
	listByChannel := map[string]string{}
	for _, c := range poster.calls {
		if c.threadTS != "" {
			listByChannel[c.channel] = sectionTextOf(c.msg)
		}
	}
	cases := []struct{ channel, url, other string }{
		{"C_ACME", "github.com/acme/api/pull/42", "github.com/beta/web/pull/42"},
		{"C_BETA", "github.com/beta/web/pull/42", "github.com/acme/api/pull/42"},
	}
	for _, c := range cases {
		list := listByChannel[c.channel]
		if strings.Count(list, c.url) != 1 {
			t.Errorf("%s list should contain %s exactly once; got %d in:\n%s",
				c.channel, c.url, strings.Count(list, c.url), list)
		}
		if strings.Contains(list, c.other) {
			t.Errorf("%s list leaked the other channel's PR %s:\n%s", c.channel, c.other, list)
		}
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
