package application

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

func boolPtr(value bool) *bool { return &value }

func TestResolveRouting_RepoOverridesStar(t *testing.T) {
	star := &domain.RepoConfig{Channel: "C0STAR", Mentions: []string{"<@S>"}, MentionsPresent: true}
	repo := &domain.RepoConfig{Channel: "C0REPO"}

	got := resolveRouting(star, repo)

	assert.Equal(t, "C0REPO", got.Channel)
	assert.Equal(t, []string{"<@S>"}, got.Mentions, "repo omits mentions, so the star tier's survive")
}

func TestResolveRouting_RepoInheritsChannel(t *testing.T) {
	star := &domain.RepoConfig{Channel: "C0STAR"}
	repo := &domain.RepoConfig{Mentions: []string{"<@U>"}, MentionsPresent: true}

	got := resolveRouting(star, repo)

	assert.Equal(t, "C0STAR", got.Channel)
	assert.Equal(t, []string{"<@U>"}, got.Mentions)
}

func TestResolveRouting_NoMentionsAnywhere_DefaultsChannelPing(t *testing.T) {
	got := resolveRouting(nil, &domain.RepoConfig{Channel: "C0REPO"})

	assert.Equal(t, []string{domain.ChannelMention}, got.Mentions)
}

func TestResolveRouting_EmptyMentionsPresent_PingsNobody(t *testing.T) {
	repo := &domain.RepoConfig{Channel: "C0REPO", Mentions: []string{}, MentionsPresent: true}

	got := resolveRouting(nil, repo)

	assert.Empty(t, got.Mentions)
}

func TestResolveRouting_StarOnly(t *testing.T) {
	got := resolveRouting(&domain.RepoConfig{Channel: "C0STAR"}, nil)

	assert.Equal(t, "C0STAR", got.Channel)
	assert.Equal(t, []string{domain.ChannelMention}, got.Mentions)
}

func TestResolveBehavior_RepoOverridesStarOverridesGlobal(t *testing.T) {
	global := domain.Defaults{
		Reactions:        domain.Reactions{Enabled: true, NewPR: "eyes", Approved: "white_check_mark", MergedPR: "merge"},
		IgnoreAIReviews:  false,
		DependabotFormat: true,
	}
	shipit := "shipit"
	star := &domain.RepoConfig{Reactions: &domain.ReactionsOverride{Approved: &shipit}}
	disabled := false
	repo := &domain.RepoConfig{
		Reactions:       &domain.ReactionsOverride{Enabled: &disabled},
		IgnoreAIReviews: boolPtr(true),
	}

	reactions, ignoreAIReviews, dependabotFormat := resolveBehavior(global, star, repo)

	assert.Equal(t, "shipit", reactions.Approved, "star tier wins over global")
	assert.Equal(t, "eyes", reactions.NewPR, "nobody overrode new_pr")
	assert.False(t, reactions.Enabled, "repo tier wins over star and global")
	assert.True(t, ignoreAIReviews)
	assert.True(t, dependabotFormat)
}

func TestResolveBehavior_AllGlobalWhenNoTiers(t *testing.T) {
	global := domain.Defaults{Reactions: domain.Reactions{Enabled: true, NewPR: "eyes"}, DependabotFormat: true}

	reactions, ignoreAIReviews, dependabotFormat := resolveBehavior(global, nil, nil)

	assert.Equal(t, "eyes", reactions.NewPR)
	assert.True(t, reactions.Enabled)
	assert.False(t, ignoreAIReviews)
	assert.True(t, dependabotFormat)
}

func TestResolveBaseTargets_SingleForm(t *testing.T) {
	repo := &domain.RepoConfig{Channel: "C0WEB", Mentions: []string{"<@U0A>"}, MentionsPresent: true}

	got := resolveBaseTargets(nil, repo)

	require.Len(t, got, 1)
	assert.Equal(t, "C0WEB", got[0].Channel)
	assert.Equal(t, []string{"<@U0A>"}, got[0].Mentions)
}

func TestResolveBaseTargets_ListForm(t *testing.T) {
	repo := &domain.RepoConfig{Channels: []domain.ChannelSpec{
		{Channel: "C0API1", Mentions: []string{"<@U0A>"}, MentionsPresent: true},
		{Channel: "C0API2"},
	}}

	got := resolveBaseTargets(nil, repo)

	require.Len(t, got, 2)
	assert.Equal(t, "C0API1", got[0].Channel)
	assert.Equal(t, []string{"<@U0A>"}, got[0].Mentions)
	assert.Equal(t, "C0API2", got[1].Channel)
	assert.Equal(t, []string{domain.ChannelMention}, got[1].Mentions, "absent mentions fall back to @channel")
}

func TestResolveBaseTargets_RepoListReplacesStarSingle(t *testing.T) {
	star := &domain.RepoConfig{Channel: "C0STAR"}
	repo := &domain.RepoConfig{Channels: []domain.ChannelSpec{{Channel: "C0R1"}, {Channel: "C0R2"}}}

	got := resolveBaseTargets(star, repo)

	require.Len(t, got, 2)
	assert.Equal(t, "C0R1", got[0].Channel)
	assert.Equal(t, "C0R2", got[1].Channel)
}

func TestResolveBaseTargets_ExplicitEmptyMentionsListForm(t *testing.T) {
	repo := &domain.RepoConfig{Channels: []domain.ChannelSpec{
		{Channel: "C0API2", Mentions: []string{}, MentionsPresent: true},
	}}

	got := resolveBaseTargets(nil, repo)

	require.Len(t, got, 1)
	assert.Empty(t, got[0].Mentions)
}
