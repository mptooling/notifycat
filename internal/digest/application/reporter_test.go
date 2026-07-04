package application_test

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/digest/application"
	"github.com/mptooling/notifycat/internal/digest/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeFinder struct{ prs []domain.PullRequest }

func (f fakeFinder) FindStuck(context.Context, time.Time) ([]domain.PullRequest, error) {
	return f.prs, nil
}

// recordingFinder captures the cutoff it was asked for so a test can assert the
// digest computed "start of day" in the configured timezone.
type recordingFinder struct{ cutoff time.Time }

func (f *recordingFinder) FindStuck(_ context.Context, cutoff time.Time) ([]domain.PullRequest, error) {
	f.cutoff = cutoff
	return nil, nil
}

type fakeMappings struct {
	byRepo map[string]routingdomain.RepoMapping
	// base is returned for every repository when byRepo is nil.
	base routingdomain.RepoMapping
}

func (f fakeMappings) Get(_ context.Context, repo string) (routingdomain.RepoMapping, error) {
	if f.byRepo != nil {
		m, ok := f.byRepo[repo]
		if !ok {
			return routingdomain.RepoMapping{}, routingdomain.ErrNotFound
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
	// Default: enabled with 9am schedule.
	return routingdomain.DigestConfig{Enabled: true, Schedule: "0 9 * * *"}
}

// fakeComposer records what the reporter asked it to render (mentions + count
// for parents, the StuckPR rows for lists) and returns sentinel Messages. The
// Slack text formatting is the composer's own concern and is covered in
// internal/slack; here we assert the reporter hands its ports the right data.
type parentRender struct {
	mentions []string
	count    int
}

type listRender struct {
	prs []domain.StuckPR
}

type fakeComposer struct {
	parents []parentRender
	lists   []listRender
}

func (c *fakeComposer) StuckDigestParent(mentions []string, count int) domain.Message {
	c.parents = append(c.parents, parentRender{mentions: mentions, count: count})
	return domain.Message{Fallback: "parent"}
}

func (c *fakeComposer) StuckDigestList(prs []domain.StuckPR) domain.Message {
	c.lists = append(c.lists, listRender{prs: prs})
	return domain.Message{Fallback: "list"}
}

type postCall struct {
	channel  string
	threadTS string // "" for a parent post, the parent ts for a reply
	msg      domain.Message
}

type fakePoster struct{ calls []postCall }

func (f *fakePoster) PostMessage(_ context.Context, channel string, msg domain.Message) (string, error) {
	f.calls = append(f.calls, postCall{channel: channel, msg: msg})
	return "ts-" + channel, nil
}

func (f *fakePoster) PostReply(_ context.Context, channel, threadTS string, msg domain.Message) (string, error) {
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

// parentChannelOrder returns the channels of parent (non-threaded) posts in the
// order they were posted, so a test can correlate the composer's parent renders
// (which don't carry the channel) to their channels.
func (f *fakePoster) parentChannelOrder() []string {
	var order []string
	for _, c := range f.calls {
		if c.threadTS == "" {
			order = append(order, c.channel)
		}
	}
	return order
}

func repoSet(prs []domain.StuckPR) map[string]bool {
	m := map[string]bool{}
	for _, pr := range prs {
		m[pr.Repository] = true
	}
	return m
}

func newTestReporter(finder domain.StuckFinder, mappings domain.MappingLookup, poster domain.DigestPoster, composer domain.DigestComposer, digests domain.DigestResolver, tz *time.Location, now func() time.Time) *application.Reporter {
	return application.NewReporter(domain.ReporterParams{
		Finder:   finder,
		Mappings: mappings,
		Poster:   poster,
		Composer: composer,
		Digests:  digests,
		Logger:   discardLogger(),
		TZ:       tz,
		Now:      now,
	})
}

func TestReporter_Report_PostsParentThenThreadedListPerChannel(t *testing.T) {
	now := time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local)
	twoDaysAgo := time.Date(2026, 6, 6, 12, 0, 0, 0, time.Local)

	finder := fakeFinder{prs: []domain.PullRequest{
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t1"}}},
		{PRNumber: 51, Repository: "acme/web", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t2"}}},
		{PRNumber: 88, Repository: "beta/x", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_BETA", MessageID: "t3"}}},
		{PRNumber: 99, Repository: "ghost/unmapped", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_GHOST", MessageID: "t4"}}},
	}}
	mapp := fakeMappings{byRepo: map[string]routingdomain.RepoMapping{
		"acme/api": {Repository: "acme/api", SlackChannel: "C_ACME", Mentions: []string{"<!channel>"}},
		"acme/web": {Repository: "acme/web", SlackChannel: "C_ACME", Mentions: []string{"<!channel>"}},
		"beta/x":   {Repository: "beta/x", SlackChannel: "C_BETA", Mentions: []string{"<@U1>"}},
	}}
	digests := fakeDigestResolver{digests: map[string]routingdomain.DigestConfig{
		"acme/api": {Enabled: true, Schedule: "0 9 * * *"},
		"acme/web": {Enabled: true, Schedule: "0 9 * * *"},
		"beta/x":   {Enabled: true, Schedule: "0 9 * * *"},
	}}
	composer := &fakeComposer{}
	poster := &fakePoster{}

	r := newTestReporter(finder, mapp, poster, composer, digests, time.Local, func() time.Time { return now })
	if err := r.Report(context.Background()); err != nil {
		t.Fatalf("Report: %v", err)
	}

	// Two channels × (one parent + one threaded list reply), first-seen order.
	if len(poster.calls) != 4 {
		t.Fatalf("posted %d messages; want 4 (parent + list per channel)", len(poster.calls))
	}
	if poster.calls[0].channel != "C_ACME" || poster.calls[0].threadTS != "" {
		t.Fatalf("call[0] = %+v; want a C_ACME parent post", poster.calls[0])
	}
	if poster.calls[1].channel != "C_ACME" || poster.calls[1].threadTS != "ts-C_ACME" {
		t.Fatalf("call[1] = %+v; want a C_ACME reply threaded on ts-C_ACME", poster.calls[1])
	}
	if poster.calls[2].channel != "C_BETA" || poster.calls[2].threadTS != "" {
		t.Fatalf("call[2] = %+v; want a C_BETA parent post", poster.calls[2])
	}
	if poster.calls[3].channel != "C_BETA" || poster.calls[3].threadTS != "ts-C_BETA" {
		t.Fatalf("call[3] = %+v; want a C_BETA reply threaded on ts-C_BETA", poster.calls[3])
	}

	// The parent headline data per channel (mentions + count), correlated by order.
	if len(composer.parents) != 2 {
		t.Fatalf("composed %d parents; want 2", len(composer.parents))
	}
	if got := composer.parents[0]; got.count != 2 || !reflect.DeepEqual(got.mentions, []string{"<!channel>"}) {
		t.Errorf("acme parent = %+v; want count 2, mentions [<!channel>]", got)
	}
	if got := composer.parents[1]; got.count != 1 || !reflect.DeepEqual(got.mentions, []string{"<@U1>"}) {
		t.Errorf("beta parent = %+v; want count 1, mentions [<@U1>]", got)
	}

	// The list rows — with reconstructed URL and idle-days as structured data.
	if len(composer.lists) != 2 {
		t.Fatalf("composed %d lists; want 2", len(composer.lists))
	}
	wantAcme := []domain.StuckPR{
		{Repository: "acme/api", Number: 42, URL: "https://github.com/acme/api/pull/42", IdleDays: 2},
		{Repository: "acme/web", Number: 51, URL: "https://github.com/acme/web/pull/51", IdleDays: 2},
	}
	if !reflect.DeepEqual(composer.lists[0].prs, wantAcme) {
		t.Errorf("acme list = %+v; want %+v", composer.lists[0].prs, wantAcme)
	}
	wantBeta := []domain.StuckPR{
		{Repository: "beta/x", Number: 88, URL: "https://github.com/beta/x/pull/88", IdleDays: 2},
	}
	if !reflect.DeepEqual(composer.lists[1].prs, wantBeta) {
		t.Errorf("beta list = %+v; want %+v", composer.lists[1].prs, wantBeta)
	}

	// ghost/unmapped never reaches the composer.
	for _, l := range composer.lists {
		for _, pr := range l.prs {
			if pr.Repository == "ghost/unmapped" {
				t.Errorf("unmapped repo leaked into a list: %+v", pr)
			}
		}
	}
}

// Each stuck PR must surface in exactly one channel's list. The same PR number
// living in two different repos is two distinct PRs, so each belongs only in its
// own repo's channel — the identity is (repo, number), never the number alone.
func TestReporter_Report_NoPRDuplicatedAcrossChannels(t *testing.T) {
	now := time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local)
	twoDaysAgo := time.Date(2026, 6, 6, 12, 0, 0, 0, time.Local)

	finder := fakeFinder{prs: []domain.PullRequest{
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t1"}}},
		{PRNumber: 42, Repository: "beta/web", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_BETA", MessageID: "t2"}}}, // same number, different repo
	}}
	mapp := fakeMappings{byRepo: map[string]routingdomain.RepoMapping{
		"acme/api": {Repository: "acme/api", SlackChannel: "C_ACME"},
		"beta/web": {Repository: "beta/web", SlackChannel: "C_BETA"},
	}}
	digests := fakeDigestResolver{digests: map[string]routingdomain.DigestConfig{
		"acme/api": {Enabled: true, Schedule: "0 9 * * *"},
		"beta/web": {Enabled: true, Schedule: "0 9 * * *"},
	}}
	composer := &fakeComposer{}
	poster := &fakePoster{}

	r := newTestReporter(finder, mapp, poster, composer, digests, time.Local, func() time.Time { return now })
	if err := r.Report(context.Background()); err != nil {
		t.Fatalf("Report: %v", err)
	}

	order := poster.parentChannelOrder()
	if len(composer.lists) != len(order) {
		t.Fatalf("lists %d != parent channels %d", len(composer.lists), len(order))
	}
	listByChannel := map[string][]domain.StuckPR{}
	for i, l := range composer.lists {
		listByChannel[order[i]] = l.prs
	}
	assertOnly := func(channel, repo string, number int) {
		prs := listByChannel[channel]
		if len(prs) != 1 || prs[0].Repository != repo || prs[0].Number != number {
			t.Errorf("%s list = %+v; want exactly %s #%d", channel, prs, repo, number)
		}
	}
	assertOnly("C_ACME", "acme/api", 42)
	assertOnly("C_BETA", "beta/web", 42)
}

func TestReporter_Report_NoStuckPRsPostsNothing(t *testing.T) {
	composer := &fakeComposer{}
	poster := &fakePoster{}
	r := newTestReporter(fakeFinder{}, fakeMappings{byRepo: map[string]routingdomain.RepoMapping{}}, poster, composer, fakeDigestResolver{}, time.Local, time.Now)

	if err := r.Report(context.Background()); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(poster.calls) != 0 {
		t.Fatalf("posted %d digests on an empty result; want 0", len(poster.calls))
	}
	if len(composer.parents)+len(composer.lists) != 0 {
		t.Fatalf("composed messages on an empty result; want none")
	}
}

func TestReporter_ReportSchedule_FiltersReposBySchedule(t *testing.T) {
	now := time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local)
	twoDaysAgo := time.Date(2026, 6, 6, 12, 0, 0, 0, time.Local)

	finder := fakeFinder{prs: []domain.PullRequest{
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t1"}}},      // 9am schedule
		{PRNumber: 51, Repository: "acme/web", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t2"}}},      // 6pm schedule
		{PRNumber: 88, Repository: "beta/disabled", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_BETA", MessageID: "t3"}}}, // disabled
	}}
	mapp := fakeMappings{byRepo: map[string]routingdomain.RepoMapping{
		"acme/api":      {Repository: "acme/api", SlackChannel: "C_ACME", Mentions: []string{"<!channel>"}},
		"acme/web":      {Repository: "acme/web", SlackChannel: "C_ACME", Mentions: []string{"<!channel>"}},
		"beta/disabled": {Repository: "beta/disabled", SlackChannel: "C_BETA", Mentions: []string{"<@U1>"}},
	}}
	digests := fakeDigestResolver{digests: map[string]routingdomain.DigestConfig{
		"acme/api":      {Enabled: true, Schedule: "0 9 * * *"},
		"acme/web":      {Enabled: true, Schedule: "0 18 * * *"},
		"beta/disabled": {Enabled: false, Schedule: "0 9 * * *"},
	}}
	composer := &fakeComposer{}
	poster := &fakePoster{}

	r := newTestReporter(finder, mapp, poster, composer, digests, time.Local, func() time.Time { return now })

	// Run for the 9am spec: should only include acme/api.
	if err := r.ReportSchedule(context.Background(), "0 9 * * *"); err != nil {
		t.Fatalf("ReportSchedule(0 9 * * *): %v", err)
	}
	if len(poster.calls) != 2 {
		t.Fatalf("after ReportSchedule(0 9 * * *): posted %d messages; want 2", len(poster.calls))
	}
	if poster.calls[0].channel != "C_ACME" || poster.calls[0].threadTS != "" {
		t.Fatalf("call[0] = %+v; want a C_ACME parent post", poster.calls[0])
	}
	if len(composer.lists) != 1 {
		t.Fatalf("composed %d lists; want 1", len(composer.lists))
	}
	if got := repoSet(composer.lists[0].prs); !got["acme/api"] || got["acme/web"] || got["beta/disabled"] {
		t.Errorf("9am list repos = %v; want only acme/api", got)
	}

	// Reset and run for the 6pm spec: should only include acme/web.
	composer.lists = nil
	poster.calls = nil
	if err := r.ReportSchedule(context.Background(), "0 18 * * *"); err != nil {
		t.Fatalf("ReportSchedule(0 18 * * *): %v", err)
	}
	if len(poster.calls) != 2 {
		t.Fatalf("after ReportSchedule(0 18 * * *): posted %d messages; want 2", len(poster.calls))
	}
	if len(composer.lists) != 1 {
		t.Fatalf("composed %d lists; want 1", len(composer.lists))
	}
	if got := repoSet(composer.lists[0].prs); !got["acme/web"] || got["acme/api"] || got["beta/disabled"] {
		t.Errorf("6pm list repos = %v; want only acme/web", got)
	}
}

func TestReporter_CutoffHonorsTimezone(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	rec := &recordingFinder{}
	r := newTestReporter(rec, fakeMappings{byRepo: map[string]routingdomain.RepoMapping{}}, &fakePoster{}, &fakeComposer{}, fakeDigestResolver{}, ny, func() time.Time {
		return time.Date(2026, 6, 8, 2, 0, 0, 0, time.UTC)
	})
	// 2026-06-08 02:00 UTC is 2026-06-07 22:00 in New York (EDT). The digest's
	// "start of day" cutoff must therefore be 2026-06-07 00:00 NY, not 06-08:
	// the configured zone — not the instant's own zone — drives the boundary.
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
	finder := fakeFinder{prs: []domain.PullRequest{{
		Repository: "acme/mono", PRNumber: 7, UpdatedAt: longAgo,
		Messages: []domain.MessageRef{
			{Channel: "C0BASE", MessageID: "1"},
			{Channel: "C0AUTH", MessageID: "2"},
		},
	}}}
	mapp := fakeMappings{base: routingdomain.RepoMapping{SlackChannel: "C0BASE", Mentions: []string{"<!subteam^S0ENG>"}}}
	composer := &fakeComposer{}
	poster := &fakePoster{}
	r := newTestReporter(finder, mapp, poster, composer, fakeDigestResolver{}, time.UTC, time.Now)

	if err := r.Report(context.Background()); err != nil {
		t.Fatalf("Report: %v", err)
	}
	posted := poster.channels()
	if !posted["C0BASE"] || !posted["C0AUTH"] {
		t.Fatalf("want a reminder in each stored channel; got %+v", posted)
	}

	// Correlate parent renders to channels by first-seen post order.
	order := poster.parentChannelOrder()
	mentionsByChannel := map[string][]string{}
	for i, p := range composer.parents {
		mentionsByChannel[order[i]] = p.mentions
	}
	// Base mentions only on the base channel.
	if got := mentionsByChannel["C0AUTH"]; len(got) != 0 {
		t.Errorf("path channel should get no ping without stored mentions; got %v", got)
	}
	if got := mentionsByChannel["C0BASE"]; len(got) == 0 {
		t.Errorf("base channel should carry base mentions; got none")
	}
}
