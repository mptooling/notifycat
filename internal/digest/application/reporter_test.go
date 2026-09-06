package application_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/digest/application"
	"github.com/mptooling/notifycat/internal/digest/domain"
	"github.com/mptooling/notifycat/internal/kernel"
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

// fakeTargets stands in for the routing provider's base fan-out. An absent
// repository yields no targets, which is how an unmapped repo reaches the
// reporter.
type fakeTargets struct {
	byRepo map[string][]routingdomain.Target
	// base is returned for every repository when byRepo is nil.
	base []routingdomain.Target
}

func (f fakeTargets) BaseTargets(repository string) []routingdomain.Target {
	if f.byRepo != nil {
		return f.byRepo[repository]
	}
	return f.base
}

// channelTargets builds a single-channel target set, the shape a repo tier with
// one `channel:` resolves to.
func channelTargets(channel string, mentions ...string) []routingdomain.Target {
	return []routingdomain.Target{{Channel: channel, Mentions: mentions}}
}

type fakeDigestResolver struct {
	digests map[string]routingdomain.DigestConfig
}

func (f fakeDigestResolver) DigestFor(repository string) routingdomain.DigestConfig {
	if digest, ok := f.digests[repository]; ok {
		return digest
	}
	return routingdomain.DigestConfig{Enabled: true, Schedule: "0 9 * * *"}
}

// fakeComposer records what the reporter asked it to render (mentions + count
// for parents, the StuckPR rows for lists) and returns sentinel Messages. The
// Slack text formatting is the composer's own concern and is covered in
// internal/platform/slack; here we assert the reporter hands its ports the right
// data.
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
	message  domain.Message
}

type fakePoster struct{ calls []postCall }

func (f *fakePoster) PostMessage(_ context.Context, channel string, message domain.Message) (string, error) {
	f.calls = append(f.calls, postCall{channel: channel, message: message})
	return "ts-" + channel, nil
}

func (f *fakePoster) PostReply(_ context.Context, channel, threadTS string, message domain.Message) (string, error) {
	f.calls = append(f.calls, postCall{channel: channel, threadTS: threadTS, message: message})
	return "reply-" + channel, nil
}

// channels returns every channel that received at least one post.
func (f *fakePoster) channels() []string {
	var posted []string
	for _, call := range f.calls {
		posted = append(posted, call.channel)
	}
	return posted
}

// parentChannelOrder returns the channels of parent (non-threaded) posts in the
// order they were posted, so a test can correlate the composer's parent renders
// (which don't carry the channel) to their channels.
func (f *fakePoster) parentChannelOrder() []string {
	var order []string
	for _, call := range f.calls {
		if call.threadTS == "" {
			order = append(order, call.channel)
		}
	}
	return order
}

// mentionsByChannel correlates each parent render to the channel it was posted to.
func mentionsByChannel(poster *fakePoster, composer *fakeComposer) map[string][]string {
	mentions := map[string][]string{}
	for i, channel := range poster.parentChannelOrder() {
		mentions[channel] = composer.parents[i].mentions
	}
	return mentions
}

// listsByChannel correlates each composed list to the channel it was posted to.
func listsByChannel(poster *fakePoster, composer *fakeComposer) map[string][]domain.StuckPR {
	lists := map[string][]domain.StuckPR{}
	for i, channel := range poster.parentChannelOrder() {
		lists[channel] = composer.lists[i].prs
	}
	return lists
}

func listedRepositories(prs []domain.StuckPR) []string {
	repositories := make([]string, len(prs))
	for i, pullRequest := range prs {
		repositories[i] = pullRequest.Repository
	}
	return repositories
}

func newTestReporter(
	finder domain.StuckFinder,
	targets domain.DigestTargets,
	poster domain.DigestPoster,
	composer domain.DigestComposer,
	digests domain.DigestResolver,
	location *time.Location,
	now func() time.Time,
) *application.Reporter {
	return application.NewReporter(domain.ReporterParams{
		Finder:   finder,
		Targets:  targets,
		Poster:   poster,
		Composer: composer,
		Digests:  digests,
		Logger:   discardLogger(),
		TZ:       location,
		Now:      now,
	})
}

var (
	reportNow  = time.Date(2026, 6, 8, 9, 0, 0, 0, time.Local)
	twoDaysAgo = time.Date(2026, 6, 6, 12, 0, 0, 0, time.Local)
)

func dailyDigest(repositories ...string) fakeDigestResolver {
	digests := map[string]routingdomain.DigestConfig{}
	for _, repository := range repositories {
		digests[repository] = routingdomain.DigestConfig{Enabled: true, Schedule: "0 9 * * *"}
	}
	return fakeDigestResolver{digests: digests}
}

func TestReporter_Report_BitbucketProviderBuildsBitbucketURLs(t *testing.T) {
	finder := fakeFinder{prs: []domain.PullRequest{
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo},
	}}
	targets := fakeTargets{byRepo: map[string][]routingdomain.Target{
		"acme/api": channelTargets("C_ACME"),
	}}
	composer := &fakeComposer{}
	reporter := application.NewReporter(domain.ReporterParams{
		Finder:   finder,
		Targets:  targets,
		Poster:   &fakePoster{},
		Composer: composer,
		Digests:  dailyDigest("acme/api"),
		Logger:   discardLogger(),
		TZ:       time.Local,
		Now:      func() time.Time { return reportNow },
		Provider: kernel.ProviderBitbucket,
	})

	err := reporter.Report(context.Background())

	require.NoError(t, err)
	require.Len(t, composer.lists, 1)
	assert.Equal(t, []domain.StuckPR{
		{Repository: "acme/api", Number: 42, URL: "https://bitbucket.org/acme/api/pull-requests/42", IdleDays: 2},
	}, composer.lists[0].prs)
}

func TestReporter_Report_PostsParentThenThreadedListPerChannel(t *testing.T) {
	finder := fakeFinder{prs: []domain.PullRequest{
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo},
		{PRNumber: 51, Repository: "acme/web", UpdatedAt: twoDaysAgo},
		{PRNumber: 88, Repository: "beta/x", UpdatedAt: twoDaysAgo},
		{PRNumber: 99, Repository: "ghost/unmapped", UpdatedAt: twoDaysAgo},
	}}
	targets := fakeTargets{byRepo: map[string][]routingdomain.Target{
		"acme/api": channelTargets("C_ACME", "<!channel>"),
		"acme/web": channelTargets("C_ACME", "<!channel>"),
		"beta/x":   channelTargets("C_BETA", "<@U1>"),
	}}
	composer := &fakeComposer{}
	poster := &fakePoster{}
	reporter := newTestReporter(finder, targets, poster, composer, dailyDigest("acme/api", "acme/web", "beta/x"), time.Local, func() time.Time { return reportNow })

	err := reporter.Report(context.Background())

	require.NoError(t, err)
	// Two channels × (one parent + one threaded list reply), first-seen order.
	require.Len(t, poster.calls, 4)
	assert.Equal(t, postCall{channel: "C_ACME", message: domain.Message{Fallback: "parent"}}, poster.calls[0])
	assert.Equal(t, postCall{channel: "C_ACME", threadTS: "ts-C_ACME", message: domain.Message{Fallback: "list"}}, poster.calls[1])
	assert.Equal(t, postCall{channel: "C_BETA", message: domain.Message{Fallback: "parent"}}, poster.calls[2])
	assert.Equal(t, postCall{channel: "C_BETA", threadTS: "ts-C_BETA", message: domain.Message{Fallback: "list"}}, poster.calls[3])

	require.Len(t, composer.parents, 2)
	assert.Equal(t, parentRender{mentions: []string{"<!channel>"}, count: 2}, composer.parents[0])
	assert.Equal(t, parentRender{mentions: []string{"<@U1>"}, count: 1}, composer.parents[1])

	lists := listsByChannel(poster, composer)
	assert.Equal(t, []domain.StuckPR{
		{Repository: "acme/api", Number: 42, URL: "https://github.com/acme/api/pull/42", IdleDays: 2},
		{Repository: "acme/web", Number: 51, URL: "https://github.com/acme/web/pull/51", IdleDays: 2},
	}, lists["C_ACME"])
	assert.Equal(t, []domain.StuckPR{
		{Repository: "beta/x", Number: 88, URL: "https://github.com/beta/x/pull/88", IdleDays: 2},
	}, lists["C_BETA"])
	assert.NotContains(t, poster.channels(), "C_GHOST", "an unmapped repo never reaches Slack")
}

// Each stuck PR must surface in exactly one channel's list. The same PR number
// living in two different repos is two distinct PRs, so each belongs only in its
// own repo's channel — the identity is (repo, number), never the number alone.
func TestReporter_Report_NoPRDuplicatedAcrossChannels(t *testing.T) {
	finder := fakeFinder{prs: []domain.PullRequest{
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo},
		{PRNumber: 42, Repository: "beta/web", UpdatedAt: twoDaysAgo},
	}}
	targets := fakeTargets{byRepo: map[string][]routingdomain.Target{
		"acme/api": channelTargets("C_ACME"),
		"beta/web": channelTargets("C_BETA"),
	}}
	composer := &fakeComposer{}
	poster := &fakePoster{}
	reporter := newTestReporter(finder, targets, poster, composer, dailyDigest("acme/api", "beta/web"), time.Local, func() time.Time { return reportNow })

	err := reporter.Report(context.Background())

	require.NoError(t, err)
	lists := listsByChannel(poster, composer)
	assert.Equal(t, []domain.StuckPR{
		{Repository: "acme/api", Number: 42, URL: "https://github.com/acme/api/pull/42", IdleDays: 2},
	}, lists["C_ACME"])
	assert.Equal(t, []domain.StuckPR{
		{Repository: "beta/web", Number: 42, URL: "https://github.com/beta/web/pull/42", IdleDays: 2},
	}, lists["C_BETA"])
}

func TestReporter_Report_NoStuckPRsPostsNothing(t *testing.T) {
	composer := &fakeComposer{}
	poster := &fakePoster{}
	reporter := newTestReporter(fakeFinder{}, fakeTargets{byRepo: map[string][]routingdomain.Target{}}, poster, composer, fakeDigestResolver{}, time.Local, time.Now)

	err := reporter.Report(context.Background())

	require.NoError(t, err)
	assert.Empty(t, poster.calls)
	assert.Empty(t, composer.parents)
	assert.Empty(t, composer.lists)
}

func TestReporter_ReportSchedule_FiltersReposBySchedule(t *testing.T) {
	finder := fakeFinder{prs: []domain.PullRequest{
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo},
		{PRNumber: 51, Repository: "acme/web", UpdatedAt: twoDaysAgo},
		{PRNumber: 88, Repository: "beta/disabled", UpdatedAt: twoDaysAgo},
	}}
	targets := fakeTargets{byRepo: map[string][]routingdomain.Target{
		"acme/api":      channelTargets("C_ACME", "<!channel>"),
		"acme/web":      channelTargets("C_ACME", "<!channel>"),
		"beta/disabled": channelTargets("C_BETA", "<@U1>"),
	}}
	digests := fakeDigestResolver{digests: map[string]routingdomain.DigestConfig{
		"acme/api":      {Enabled: true, Schedule: "0 9 * * *"},
		"acme/web":      {Enabled: true, Schedule: "0 18 * * *"},
		"beta/disabled": {Enabled: false, Schedule: "0 9 * * *"},
	}}
	composer := &fakeComposer{}
	poster := &fakePoster{}
	reporter := newTestReporter(finder, targets, poster, composer, digests, time.Local, func() time.Time { return reportNow })

	require.NoError(t, reporter.ReportSchedule(context.Background(), "0 9 * * *"))

	require.Len(t, poster.calls, 2, "one parent plus one threaded list")
	assert.Equal(t, "C_ACME", poster.calls[0].channel)
	assert.Empty(t, poster.calls[0].threadTS)
	require.Len(t, composer.lists, 1)
	assert.Equal(t, []string{"acme/api"}, listedRepositories(composer.lists[0].prs))

	composer.lists = nil
	poster.calls = nil
	require.NoError(t, reporter.ReportSchedule(context.Background(), "0 18 * * *"))

	require.Len(t, poster.calls, 2)
	require.Len(t, composer.lists, 1)
	assert.Equal(t, []string{"acme/web"}, listedRepositories(composer.lists[0].prs),
		"the disabled repo never appears under any schedule")
}

func TestReporter_CutoffHonorsTimezone(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	finder := &recordingFinder{}
	reporter := newTestReporter(finder, fakeTargets{byRepo: map[string][]routingdomain.Target{}}, &fakePoster{}, &fakeComposer{}, fakeDigestResolver{}, newYork, func() time.Time {
		return time.Date(2026, 6, 8, 2, 0, 0, 0, time.UTC)
	})

	require.NoError(t, reporter.Report(context.Background()))

	// 2026-06-08 02:00 UTC is 2026-06-07 22:00 in New York (EDT). The digest's
	// "start of day" cutoff must therefore be 2026-06-07 00:00 NY, not 06-08:
	// the configured zone — not the instant's own zone — drives the boundary.
	assert.True(t, finder.cutoff.Equal(time.Date(2026, 6, 7, 0, 0, 0, 0, newYork)), "cutoff = %v", finder.cutoff)
	assert.Equal(t, "America/New_York", finder.cutoff.Location().String())
}

// A repo with a `channels:` fan-out gets one reminder per configured channel,
// each carrying that channel's own mentions.
func TestReporter_Report_FansOutToEveryConfiguredChannel(t *testing.T) {
	longAgo := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	finder := fakeFinder{prs: []domain.PullRequest{
		{Repository: "acme/mono", PRNumber: 7, UpdatedAt: longAgo},
	}}
	targets := fakeTargets{byRepo: map[string][]routingdomain.Target{
		"acme/mono": {
			{Channel: "C0BASE", Mentions: []string{"<!subteam^S0ENG>"}},
			{Channel: "C0AUTH", Mentions: []string{"<@U9>"}},
		},
	}}
	composer := &fakeComposer{}
	poster := &fakePoster{}
	reporter := newTestReporter(finder, targets, poster, composer, fakeDigestResolver{}, time.UTC, time.Now)

	err := reporter.Report(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"C0BASE", "C0BASE", "C0AUTH", "C0AUTH"}, poster.channels())

	mentions := mentionsByChannel(poster, composer)
	assert.Equal(t, []string{"<!subteam^S0ENG>"}, mentions["C0BASE"])
	assert.Equal(t, []string{"<@U9>"}, mentions["C0AUTH"], "each fan-out channel pings its own mentions")
}

// The reminder follows config, not the channel the PR's message was posted to:
// after an operator repoints a repo at a new channel, the digest must nag only
// there, even while the stored message still lives in the old channel.
func TestReporter_Report_IgnoresTheChannelTheMessageLivesIn(t *testing.T) {
	finder := fakeFinder{prs: []domain.PullRequest{
		{Repository: "acme/api", PRNumber: 42, UpdatedAt: twoDaysAgo},
	}}
	targets := fakeTargets{byRepo: map[string][]routingdomain.Target{
		"acme/api": channelTargets("C_NEW"),
	}}
	composer := &fakeComposer{}
	poster := &fakePoster{}
	reporter := newTestReporter(finder, targets, poster, composer, dailyDigest("acme/api"), time.Local, func() time.Time { return reportNow })

	err := reporter.Report(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"C_NEW", "C_NEW"}, poster.channels())
}

// An unmapped repository resolves to a single target with an empty channel
// (internal/routing/application/resolve.go), which must never reach Slack.
func TestReporter_Report_SkipsEmptyChannelTarget(t *testing.T) {
	finder := fakeFinder{prs: []domain.PullRequest{
		{Repository: "ghost/unmapped", PRNumber: 1, UpdatedAt: twoDaysAgo},
	}}
	targets := fakeTargets{base: []routingdomain.Target{{Channel: "", Mentions: []string{"<!channel>"}}}}
	poster := &fakePoster{}
	reporter := newTestReporter(finder, targets, poster, &fakeComposer{}, dailyDigest("ghost/unmapped"), time.Local, func() time.Time { return reportNow })

	err := reporter.Report(context.Background())

	require.NoError(t, err)
	assert.Empty(t, poster.calls)
}
