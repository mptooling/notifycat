package digest

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
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

type fakeFinder struct{ prs []store.PullRequest }

func (f fakeFinder) FindStuck(context.Context, time.Time) ([]store.PullRequest, error) {
	return f.prs, nil
}

// recordingFinder captures the cutoff it was asked for so a test can assert the
// digest computed "start of day" in the configured timezone.
type recordingFinder struct{ cutoff time.Time }

func (f *recordingFinder) FindStuck(_ context.Context, cutoff time.Time) ([]store.PullRequest, error) {
	f.cutoff = cutoff
	return nil, nil
}

type fakeMappings struct {
	byRepo map[string]store.RepoMapping
	// base is returned for every repository when byRepo is nil.
	base store.RepoMapping
}

func (f fakeMappings) Get(_ context.Context, repo string) (store.RepoMapping, error) {
	if f.byRepo != nil {
		m, ok := f.byRepo[repo]
		if !ok {
			return store.RepoMapping{}, store.ErrNotFound
		}
		return m, nil
	}
	return f.base, nil
}

type fakeDigestResolver struct {
	digests map[string]routingdomain.DigestConfig
}

func (f fakeDigestResolver) DigestFor(repo string) routingdomain.DigestConfig {
	if d, ok := f.digests[repo]; ok {
		return d
	}
	// Default: enabled with 9am schedule
	return routingdomain.DigestConfig{Enabled: true, Schedule: "0 9 * * *"}
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

// channels returns the set of channels that received at least one post.
func (f *fakePoster) channels() map[string]bool {
	m := map[string]bool{}
	for _, c := range f.calls {
		m[c.channel] = true
	}
	return m
}

// mentionsFor returns the mention tokens from the parent (non-threaded) post for
// channel. The StuckDigestParent format is ":emoji: m1,m2, N open PR..."; this
// extracts the comma-separated mention list that precedes the count. Returns nil
// when no mentions were included.
func (f *fakePoster) mentionsFor(channel string) []string {
	for _, c := range f.calls {
		if c.channel != channel || c.threadTS != "" {
			continue
		}
		text := sectionTextOf(c.msg)
		// Skip past the leading ":emoji: " token.
		const sep = ": "
		start := strings.Index(text, sep)
		if start < 0 || start+len(sep) >= len(text) {
			return nil
		}
		rest := text[start+len(sep):]
		if !strings.HasPrefix(rest, "<") {
			return nil // no mentions prefix
		}
		// Mentions prefix ends at ", N" where N is a digit (the PR count).
		for i := 0; i < len(rest)-2; i++ {
			if rest[i] == ',' && rest[i+1] == ' ' && rest[i+2] >= '0' && rest[i+2] <= '9' {
				return strings.Split(rest[:i], ",")
			}
		}
		return nil
	}
	return nil
}

func TestReporter_Report_PostsParentThenThreadedListPerChannel(t *testing.T) {
	now := time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local)
	twoDaysAgo := time.Date(2026, 6, 6, 12, 0, 0, 0, time.Local)

	finder := fakeFinder{prs: []store.PullRequest{
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo, Messages: []store.Message{{Channel: "C_ACME", MessageID: "t1"}}},
		{PRNumber: 51, Repository: "acme/web", UpdatedAt: twoDaysAgo, Messages: []store.Message{{Channel: "C_ACME", MessageID: "t2"}}},
		{PRNumber: 88, Repository: "beta/x", UpdatedAt: twoDaysAgo, Messages: []store.Message{{Channel: "C_BETA", MessageID: "t3"}}},
		{PRNumber: 99, Repository: "ghost/unmapped", UpdatedAt: twoDaysAgo, Messages: []store.Message{{Channel: "C_GHOST", MessageID: "t4"}}},
	}}
	mapp := fakeMappings{byRepo: map[string]store.RepoMapping{
		"acme/api": {Repository: "acme/api", SlackChannel: "C_ACME", Mentions: []string{"<!channel>"}},
		"acme/web": {Repository: "acme/web", SlackChannel: "C_ACME", Mentions: []string{"<!channel>"}},
		"beta/x":   {Repository: "beta/x", SlackChannel: "C_BETA", Mentions: []string{"<@U1>"}},
	}}
	digests := fakeDigestResolver{digests: map[string]routingdomain.DigestConfig{
		"acme/api": {Enabled: true, Schedule: "0 9 * * *"},
		"acme/web": {Enabled: true, Schedule: "0 9 * * *"},
		"beta/x":   {Enabled: true, Schedule: "0 9 * * *"},
	}}
	poster := &fakePoster{}

	r := NewReporter(finder, mapp, poster, slack.NewComposer("eyes"), digests, discardLogger(), time.Local)
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

	finder := fakeFinder{prs: []store.PullRequest{
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo, Messages: []store.Message{{Channel: "C_ACME", MessageID: "t1"}}},
		{PRNumber: 42, Repository: "beta/web", UpdatedAt: twoDaysAgo, Messages: []store.Message{{Channel: "C_BETA", MessageID: "t2"}}}, // same number, different repo
	}}
	mapp := fakeMappings{byRepo: map[string]store.RepoMapping{
		"acme/api": {Repository: "acme/api", SlackChannel: "C_ACME"},
		"beta/web": {Repository: "beta/web", SlackChannel: "C_BETA"},
	}}
	digests := fakeDigestResolver{digests: map[string]routingdomain.DigestConfig{
		"acme/api": {Enabled: true, Schedule: "0 9 * * *"},
		"beta/web": {Enabled: true, Schedule: "0 9 * * *"},
	}}
	poster := &fakePoster{}

	r := NewReporter(finder, mapp, poster, slack.NewComposer("eyes"), digests, discardLogger(), time.Local)
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
	r := NewReporter(fakeFinder{}, fakeMappings{byRepo: map[string]store.RepoMapping{}}, poster, slack.NewComposer("eyes"), fakeDigestResolver{}, discardLogger(), time.Local)

	if err := r.Report(context.Background()); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(poster.calls) != 0 {
		t.Fatalf("posted %d digests on an empty result; want 0", len(poster.calls))
	}
}

func TestReporter_ReportSchedule_FiltersReposBySchedule(t *testing.T) {
	now := time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local)
	twoDaysAgo := time.Date(2026, 6, 6, 12, 0, 0, 0, time.Local)

	finder := fakeFinder{prs: []store.PullRequest{
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo, Messages: []store.Message{{Channel: "C_ACME", MessageID: "t1"}}},      // 9am schedule
		{PRNumber: 51, Repository: "acme/web", UpdatedAt: twoDaysAgo, Messages: []store.Message{{Channel: "C_ACME", MessageID: "t2"}}},      // 6pm schedule
		{PRNumber: 88, Repository: "beta/disabled", UpdatedAt: twoDaysAgo, Messages: []store.Message{{Channel: "C_BETA", MessageID: "t3"}}}, // disabled
	}}
	mapp := fakeMappings{byRepo: map[string]store.RepoMapping{
		"acme/api":      {Repository: "acme/api", SlackChannel: "C_ACME", Mentions: []string{"<!channel>"}},
		"acme/web":      {Repository: "acme/web", SlackChannel: "C_ACME", Mentions: []string{"<!channel>"}},
		"beta/disabled": {Repository: "beta/disabled", SlackChannel: "C_BETA", Mentions: []string{"<@U1>"}},
	}}
	digests := fakeDigestResolver{digests: map[string]routingdomain.DigestConfig{
		"acme/api":      {Enabled: true, Schedule: "0 9 * * *"},
		"acme/web":      {Enabled: true, Schedule: "0 18 * * *"},
		"beta/disabled": {Enabled: false, Schedule: "0 9 * * *"},
	}}
	poster := &fakePoster{}

	r := NewReporter(finder, mapp, poster, slack.NewComposer("eyes"), digests, discardLogger(), time.Local)
	r.now = func() time.Time { return now }

	// Run for the 9am spec: should only include acme/api.
	if err := r.ReportSchedule(context.Background(), "0 9 * * *"); err != nil {
		t.Fatalf("ReportSchedule(0 9 * * *): %v", err)
	}

	// Should have posted 2 calls: parent for C_ACME + list reply.
	if len(poster.calls) != 2 {
		t.Fatalf("after ReportSchedule(0 9 * * *): posted %d messages; want 2", len(poster.calls))
	}
	if poster.calls[0].channel != "C_ACME" || poster.calls[0].threadTS != "" {
		t.Fatalf("call[0] = %+v; want a C_ACME parent post", poster.calls[0])
	}
	if poster.calls[1].channel != "C_ACME" || poster.calls[1].threadTS != "ts-C_ACME" {
		t.Fatalf("call[1] = %+v; want a C_ACME reply", poster.calls[1])
	}

	listText := sectionTextOf(poster.calls[1].msg)
	if !strings.Contains(listText, "acme/api/pull/42") {
		t.Errorf("acme/api PR not in list: %s", listText)
	}
	if strings.Contains(listText, "acme/web/pull/51") {
		t.Errorf("acme/web PR should not appear in 9am schedule: %s", listText)
	}
	if strings.Contains(listText, "beta/disabled/pull/88") {
		t.Errorf("disabled PR should not appear: %s", listText)
	}

	// Clear and run for 6pm spec: should only include acme/web.
	poster.calls = nil
	if err := r.ReportSchedule(context.Background(), "0 18 * * *"); err != nil {
		t.Fatalf("ReportSchedule(0 18 * * *): %v", err)
	}

	if len(poster.calls) != 2 {
		t.Fatalf("after ReportSchedule(0 18 * * *): posted %d messages; want 2", len(poster.calls))
	}

	listText = sectionTextOf(poster.calls[1].msg)
	if !strings.Contains(listText, "acme/web/pull/51") {
		t.Errorf("acme/web PR not in list: %s", listText)
	}
	if strings.Contains(listText, "acme/api/pull/42") {
		t.Errorf("acme/api PR should not appear in 6pm schedule: %s", listText)
	}
	if strings.Contains(listText, "beta/disabled/pull/88") {
		t.Errorf("disabled PR should not appear: %s", listText)
	}
}

func TestReporter_CutoffHonorsTimezone(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	rec := &recordingFinder{}
	r := NewReporter(rec, fakeMappings{byRepo: map[string]store.RepoMapping{}}, &fakePoster{}, slack.NewComposer("eyes"), fakeDigestResolver{}, discardLogger(), ny)
	// 2026-06-08 02:00 UTC is 2026-06-07 22:00 in New York (EDT). The digest's
	// "start of day" cutoff must therefore be 2026-06-07 00:00 NY, not 06-08:
	// the configured zone — not the instant's own zone — drives the boundary.
	r.now = func() time.Time { return time.Date(2026, 6, 8, 2, 0, 0, 0, time.UTC) }

	if err := r.Report(context.Background()); err != nil {
		t.Fatalf("Report: %v", err)
	}

	want := time.Date(2026, 6, 7, 0, 0, 0, 0, ny)
	if !rec.cutoff.Equal(want) {
		t.Errorf("cutoff = %v; want %v (start of day in NY)", rec.cutoff, want)
	}
	if loc := rec.cutoff.Location().String(); loc != "America/New_York" {
		t.Errorf("cutoff location = %q; want America/New_York", loc)
	}
}

// TestDigest_GroupsByStoredMessageChannel verifies that a PR with messages in
// multiple channels produces a reminder in each stored channel, and that base
// mentions are only added to the repo's base channel (not fan-out channels).
func TestDigest_GroupsByStoredMessageChannel(t *testing.T) {
	longAgo := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	finder := fakeFinder{prs: []store.PullRequest{{
		Repository: "acme/mono", PRNumber: 7, UpdatedAt: longAgo,
		Messages: []store.Message{
			{Channel: "C0BASE", MessageID: "1"},
			{Channel: "C0AUTH", MessageID: "2"},
		},
	}}}
	mapp := fakeMappings{base: store.RepoMapping{SlackChannel: "C0BASE", Mentions: []string{"<!subteam^S0ENG>"}}}
	poster := &fakePoster{}
	r := NewReporter(finder, mapp, poster, slack.NewComposer("eyes"), fakeDigestResolver{}, discardLogger(), time.UTC)

	if err := r.Report(context.Background()); err != nil {
		t.Fatalf("Report: %v", err)
	}
	posted := poster.channels()
	if !posted["C0BASE"] || !posted["C0AUTH"] {
		t.Fatalf("want a reminder in each stored channel; got %+v", posted)
	}
	// Base mentions only on the base channel.
	if got := poster.mentionsFor("C0AUTH"); len(got) != 0 {
		t.Errorf("path channel should get no ping without stored mentions; got %v", got)
	}
	if got := poster.mentionsFor("C0BASE"); len(got) == 0 {
		t.Errorf("base channel should carry base mentions; got none")
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
