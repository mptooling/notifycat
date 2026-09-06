package infrastructure_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/routing/application"
	domain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/routing/infrastructure"
)

const badYAML = `
mappings:
  acme: !!invalid
`

func writeMappingsFile(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "mappings.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func tierProvider() *application.Provider {
	return application.NewProvider(domain.Defaults{}, map[string]domain.Org{
		"acme": {
			"api": {Channel: "C0API", Mentions: []string{"<@U1>"}, MentionsPresent: true},
			"*":   {Channel: "C0DEFAULT"},
		},
	}, nil)
}

func TestProvider_Load_MissingFile_ReturnsFileNotFoundError(t *testing.T) {
	_, err := infrastructure.Load("/no/such/path/mappings.yaml")

	var notFound *infrastructure.FileNotFoundError
	require.ErrorAs(t, err, &notFound)
	assert.Equal(t, "/no/such/path/mappings.yaml", notFound.Path)
	assert.ErrorIs(t, notFound.Err, os.ErrNotExist)
}

func TestProvider_Load_MalformedFile_ReturnsParseError(t *testing.T) {
	path := writeMappingsFile(t, badYAML)

	_, err := infrastructure.Load(path)

	var parseErr *infrastructure.ParseError
	require.ErrorAs(t, err, &parseErr)
	assert.Equal(t, path, parseErr.Path)
	assert.Error(t, parseErr.Err)
}

func TestGet_ExplicitRepo(t *testing.T) {
	got, err := tierProvider().Get(context.Background(), "acme/api")

	require.NoError(t, err)
	assert.Equal(t, "C0API", got.SlackChannel)
	assert.Equal(t, []string{"<@U1>"}, got.Mentions)
}

func TestGet_WildcardFallback(t *testing.T) {
	got, err := tierProvider().Get(context.Background(), "acme/unlisted")

	require.NoError(t, err)
	assert.Equal(t, "C0DEFAULT", got.SlackChannel)
	assert.Equal(t, []string{domain.ChannelMention}, got.Mentions)
}

func TestGet_NoOrgOrNoTier(t *testing.T) {
	provider := application.NewProvider(domain.Defaults{}, map[string]domain.Org{
		"acme": {"api": {Channel: "C0API"}},
	}, nil)

	_, noTierErr := provider.Get(context.Background(), "acme/other")
	_, noOrgErr := provider.Get(context.Background(), "ghost/api")

	assert.ErrorIs(t, noTierErr, domain.ErrNotFound, "no tier and no wildcard")
	assert.ErrorIs(t, noOrgErr, domain.ErrNotFound, "unknown org")
}

func TestNewProvider_BehavesLikeLoad(t *testing.T) {
	provider := application.NewProvider(domain.Defaults{}, map[string]domain.Org{
		"acme": {
			"web": {Channel: "C0123ABCDE", Mentions: []string{"<@U1>"}, MentionsPresent: true},
		},
	}, nil)

	got, err := provider.Get(context.Background(), "acme/web")

	require.NoError(t, err)
	assert.Equal(t, "C0123ABCDE", got.SlackChannel)
	assert.False(t, provider.Digest().Enabled, "a nil digest section leaves the feature off")
	assert.Equal(t, domain.DefaultDigestSchedule, provider.Digest().Schedule, "the default schedule still resolves while off")
	assert.Len(t, provider.Entries(), 1)
}

func TestEntries_PerTierWithResolvedChannel(t *testing.T) {
	provider := application.NewProvider(domain.Defaults{}, map[string]domain.Org{
		"acme": {
			"web": {},
			"api": {Channel: "C0API"},
			"*":   {Channel: "C0DEFAULT"},
		},
	}, nil)

	entries := provider.Entries()

	require.Len(t, entries, 3)
	assert.Equal(t, "acme/api", entries[0].Key(), "explicit repos come first, A→Z")
	assert.Equal(t, "C0API", entries[0].Channel)
	assert.Equal(t, "acme/web", entries[1].Key())
	assert.Equal(t, "C0DEFAULT", entries[1].Channel, "web inherits the wildcard channel")
	assert.Equal(t, "acme/*", entries[2].Key(), "the wildcard sorts last")
	assert.True(t, entries[2].Wildcard)
	assert.Equal(t, "C0DEFAULT", entries[2].Channel)
}

func TestGet_PopulatesResolvedBehavior(t *testing.T) {
	global := domain.Defaults{
		Reactions:        domain.Reactions{Enabled: true, NewPR: "eyes", Approved: "white_check_mark"},
		DependabotFormat: true,
	}
	shipit := "shipit"
	provider := application.NewProvider(global, map[string]domain.Org{
		"acme": {
			"api": {Channel: "C0API", Reactions: &domain.ReactionsOverride{Approved: &shipit}},
			"*":   {Channel: "C0DEFAULT"},
		},
	}, nil)

	got, err := provider.Get(context.Background(), "acme/api")

	require.NoError(t, err)
	assert.Equal(t, "C0API", got.SlackChannel)
	assert.Equal(t, "shipit", got.Reactions.Approved, "the repo override wins")
	assert.Equal(t, "eyes", got.Reactions.NewPR, "un-overridden globals survive")
	assert.True(t, got.DependabotFormat)
}

func TestDigestFor_RepoOverridesGlobal(t *testing.T) {
	weekdays := "0 8 * * 1-5"
	provider := application.NewProvider(domain.Defaults{}, map[string]domain.Org{
		"acme": {
			"web": {Channel: "C0WEB", Digest: &domain.DigestConfig{Enabled: true, Schedule: weekdays}},
			"*":   {Channel: "C0DEFAULT"},
		},
	}, nil) // global digest absent → default off, 9am schedule

	overridden := provider.DigestFor("acme/web")
	inherited := provider.DigestFor("acme/other")

	assert.True(t, overridden.Enabled)
	assert.Equal(t, weekdays, overridden.Schedule)
	assert.False(t, inherited.Enabled, "the wildcard tier inherits the global default (off)")
	assert.Equal(t, domain.DefaultDigestSchedule, inherited.Schedule, "the wildcard tier keeps the global default schedule")
}

func TestSchedules_DistinctEnabledOnly(t *testing.T) {
	weekdays := "0 8 * * 1-5"
	provider := application.NewProvider(domain.Defaults{}, map[string]domain.Org{
		"acme": {
			"web":  {Channel: "C0WEB", Digest: &domain.DigestConfig{Enabled: true, Schedule: weekdays}},
			"api":  {Channel: "C0API", Digest: &domain.DigestConfig{Enabled: true}}, // enabled → global default schedule
			"mute": {Channel: "C0MUTE", Digest: &domain.DigestConfig{Enabled: false}},
			"*":    {Channel: "C0DEFAULT"}, // no digest override → global default (off)
		},
	}, nil)

	got := provider.Schedules()

	assert.ElementsMatch(t, []string{weekdays, domain.DefaultDigestSchedule}, got, "a disabled tier contributes no schedule")
}
