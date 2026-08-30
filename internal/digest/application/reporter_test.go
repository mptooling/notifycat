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

type fakeMappings struct {
	byRepo map[string]routingdomain.RepoMapping
	// base is returned for every repository when byRepo is nil.
	base routingdomain.RepoMapping
}

func (f fakeMappings) Get(_ context.Context, repository string) (routingdomain.RepoMapping, error) {
	if f.byRepo != nil {
		mapping, ok := f.byRepo[repository]
		if !ok {
			return routingdomain.RepoMapping{}, routingdomain.ErrNotFound
		}
		return mapping, nil
	}
	return f.base, nil
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
	mappings domain.MappingLookup,
	poster domain.DigestPoster,
	composer domain.DigestComposer,
	digests domain.DigestResolver,
	location *time.Location,
	now func() time.Time,
) *application.Reporter {
	return application.NewReporter(domain.ReporterParams{
		Finder:   finder,
		Mappings: mappings,
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
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t1"}}},
	}}
	mappings := fakeMappings{byRepo: map[string]routingdomain.RepoMapping{
		"acme/api": {Repository: "acme/api", SlackChannel: "C_ACME"},
	}}
	composer := &fakeComposer{}
	reporter := application.NewReporter(domain.ReporterParams{
		Finder:   finder,
		Mappings: mappings,
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
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t1"}}},
		{PRNumber: 51, Repository: "acme/web", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t2"}}},
		{PRNumber: 88, Repository: "beta/x", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_BETA", MessageID: "t3"}}},
		{PRNumber: 99, Repository: "ghost/unmapped", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_GHOST", MessageID: "t4"}}},
	}}
	mappings := fakeMappings{byRepo: map[string]routingdomain.RepoMapping{
		"acme/api": {Repository: "acme/api", SlackChannel: "C_ACME", Mentions: []string{"<!channel>"}},
		"acme/web": {Repository: "acme/web", SlackChannel: "C_ACME", Mentions: []string{"<!channel>"}},
		"beta/x":   {Repository: "beta/x", SlackChannel: "C_BETA", Mentions: []string{"<@U1>"}},
	}}
	composer := &fakeComposer{}
	poster := &fakePoster{}
	reporter := newTestReporter(finder, mappings, poster, composer, dailyDigest("acme/api", "acme/web", "beta/x"), time.Local, func() time.Time { return reportNow })

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
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t1"}}},
		{PRNumber: 42, Repository: "beta/web", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_BETA", MessageID: "t2"}}},
	}}
	mappings := fakeMappings{byRepo: map[string]routingdomain.RepoMapping{
		"acme/api": {Repository: "acme/api", SlackChannel: "C_ACME"},
		"beta/web": {Repository: "beta/web", SlackChannel: "C_BETA"},
	}}
	composer := &fakeComposer{}
	poster := &fakePoster{}
	reporter := newTestReporter(finder, mappings, poster, composer, dailyDigest("acme/api", "beta/web"), time.Local, func() time.Time { return reportNow })

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
	reporter := newTestReporter(fakeFinder{}, fakeMappings{byRepo: map[string]routingdomain.RepoMapping{}}, poster, composer, fakeDigestResolver{}, time.Local, time.Now)

	err := reporter.Report(context.Background())

	require.NoError(t, err)
	assert.Empty(t, poster.calls)
	assert.Empty(t, composer.parents)
	assert.Empty(t, composer.lists)
}

func TestReporter_ReportSchedule_FiltersReposBySchedule(t *testing.T) {
	finder := fakeFinder{prs: []domain.PullRequest{
		{PRNumber: 42, Repository: "acme/api", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t1"}}},
		{PRNumber: 51, Repository: "acme/web", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_ACME", MessageID: "t2"}}},
		{PRNumber: 88, Repository: "beta/disabled", UpdatedAt: twoDaysAgo, Messages: []domain.MessageRef{{Channel: "C_BETA", MessageID: "t3"}}},
	}}
	mappings := fakeMappings{byRepo: map[string]routingdomain.RepoMapping{
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
	reporter := newTestReporter(finder, mappings, poster, composer, digests, time.Local, func() time.Time { return reportNow })

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
	reporter := newTestReporter(finder, fakeMappings{byRepo: map[string]routingdomain.RepoMapping{}}, &fakePoster{}, &fakeComposer{}, fakeDigestResolver{}, newYork, func() time.Time {
		return time.Date(2026, 6, 8, 2, 0, 0, 0, time.UTC)
	})

	require.NoError(t, reporter.Report(context.Background()))

	// 2026-06-08 02:00 UTC is 2026-06-07 22:00 in New York (EDT). The digest's
	// "start of day" cutoff must therefore be 2026-06-07 00:00 NY, not 06-08:
	// the configured zone — not the instant's own zone — drives the boundary.
	assert.True(t, finder.cutoff.Equal(time.Date(2026, 6, 7, 0, 0, 0, 0, newYork)), "cutoff = %v", finder.cutoff)
	assert.Equal(t, "America/New_York", finder.cutoff.Location().String())
}

// A PR with messages in several channels produces a reminder in each stored
// channel, and the base mentions only ping the repo's base channel.
func TestDigest_GroupsByStoredMessageChannel(t *testing.T) {
	longAgo := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	finder := fakeFinder{prs: []domain.PullRequest{{
		Repository: "acme/mono", PRNumber: 7, UpdatedAt: longAgo,
		Messages: []domain.MessageRef{
			{Channel: "C0BASE", MessageID: "1"},
			{Channel: "C0AUTH", MessageID: "2"},
		},
	}}}
	mappings := fakeMappings{base: routingdomain.RepoMapping{SlackChannel: "C0BASE", Mentions: []string{"<!subteam^S0ENG>"}}}
	composer := &fakeComposer{}
	poster := &fakePoster{}
	reporter := newTestReporter(finder, mappings, poster, composer, fakeDigestResolver{}, time.UTC, time.Now)

	err := reporter.Report(context.Background())

	require.NoError(t, err)
	assert.Subset(t, poster.channels(), []string{"C0BASE", "C0AUTH"}, "every stored channel gets a reminder")

	mentions := mentionsByChannel(poster, composer)
	assert.Empty(t, mentions["C0AUTH"], "a path channel carries no ping without stored mentions")
	assert.NotEmpty(t, mentions["C0BASE"], "the base channel carries the base mentions")
}
