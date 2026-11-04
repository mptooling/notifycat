package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func partyReviewers(value int) *int { return &value }

func partyDeadline(value time.Duration) *time.Duration { return &value }

func TestPathRule_PartyOnly(t *testing.T) {
	testCases := []struct {
		name string
		rule PathRule
		want bool
	}{
		{name: "only reviewers", rule: PathRule{Party: PartyOverride{Reviewers: partyReviewers(2)}}, want: true},
		{name: "only deadline", rule: PathRule{Party: PartyOverride{Deadline: partyDeadline(time.Hour)}}, want: true},
		{name: "party and channel", rule: PathRule{Party: PartyOverride{Reviewers: partyReviewers(2)}, Channel: "C0A"}, want: false},
		{name: "party and mentions", rule: PathRule{Party: PartyOverride{Reviewers: partyReviewers(2)}, MentionsPresent: true}, want: false},
		{name: "party and channels", rule: PathRule{Party: PartyOverride{Reviewers: partyReviewers(2)}, Channels: []ChannelSpec{{Channel: "C0A"}}}, want: false},
		{name: "empty rule", rule: PathRule{}, want: false},
		{name: "declared but empty override", rule: PathRule{Party: PartyOverride{Declared: true}}, want: true},
		{name: "declared empty override and channel", rule: PathRule{Party: PartyOverride{Declared: true}, Channel: "C0A"}, want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, testCase.rule.PartyOnly())
		})
	}
}

func TestRepoConfig_PartyOnly(t *testing.T) {
	party := PartyOverride{Reviewers: partyReviewers(2)}
	partyOnlyPath := PathRule{Dir: "billing", Party: party}
	slackPath := PathRule{Dir: "web", Channel: "C0WEB"}
	enabled := true

	testCases := []struct {
		name string
		tier RepoConfig
		want bool
	}{
		{name: "only party", tier: RepoConfig{Party: party}, want: true},
		{name: "only party-only paths", tier: RepoConfig{Paths: []PathRule{partyOnlyPath}}, want: true},
		{name: "party and party-only paths", tier: RepoConfig{Party: party, Paths: []PathRule{partyOnlyPath}}, want: true},
		{name: "party and channel", tier: RepoConfig{Party: party, Channel: "C0A"}, want: false},
		{name: "party and channels", tier: RepoConfig{Party: party, Channels: []ChannelSpec{{Channel: "C0A"}}}, want: false},
		{name: "party and mentions", tier: RepoConfig{Party: party, MentionsPresent: true}, want: false},
		{name: "party and reactions", tier: RepoConfig{Party: party, Reactions: &ReactionsOverride{}}, want: false},
		{name: "party and ignore ai reviews", tier: RepoConfig{Party: party, IgnoreAIReviews: &enabled}, want: false},
		{name: "party and dependabot format", tier: RepoConfig{Party: party, DependabotFormat: &enabled}, want: false},
		{name: "party and delete on close", tier: RepoConfig{Party: party, DeleteOnClose: &enabled}, want: false},
		{name: "party and digest", tier: RepoConfig{Party: party, Digest: &DigestConfig{}}, want: false},
		{name: "party and notifications", tier: RepoConfig{Party: party, Notifications: &NotificationsConfig{}}, want: false},
		{name: "party and slack path", tier: RepoConfig{Party: party, Paths: []PathRule{slackPath}}, want: false},
		{name: "party-only path and slack path", tier: RepoConfig{Paths: []PathRule{partyOnlyPath, slackPath}}, want: false},
		{name: "empty tier", tier: RepoConfig{}, want: false},
		{name: "declared but empty party", tier: RepoConfig{Party: PartyOverride{Declared: true}}, want: true},
		{name: "declared empty party and channel", tier: RepoConfig{Party: PartyOverride{Declared: true}, Channel: "C0A"}, want: false},
		{name: "declared empty party-only path", tier: RepoConfig{Paths: []PathRule{{Dir: "billing", Party: PartyOverride{Declared: true}}}}, want: true},
		{name: "declared empty party path and slack path", tier: RepoConfig{Paths: []PathRule{{Dir: "billing", Party: PartyOverride{Declared: true}}, slackPath}}, want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, testCase.tier.PartyOnly())
		})
	}
}

func TestPartyOverride_IsSet(t *testing.T) {
	assert.False(t, PartyOverride{}.IsSet())
	assert.True(t, PartyOverride{Reviewers: partyReviewers(1)}.IsSet())
	assert.True(t, PartyOverride{Deadline: partyDeadline(time.Hour)}.IsSet())
	assert.False(t, PartyOverride{Declared: true}.IsSet(), "a declared empty override stays inert")
}
