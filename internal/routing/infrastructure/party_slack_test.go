package infrastructure_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

const partyDocument = `
mappings:
  acme:
    "*":
      channel: C0STAR00000
      party_reviewers: 2
    the-monorepo:
      channel: C0BASE00000
      party_deadline: 2h
      paths:
        services: {mentions: ["<@U0S>"]}
        services/billing: {party_reviewers: 4}
        docs: {channel: C0DOCS00000, party_reviewers: 1}
        tools: {party_deadline: 30m}
    web:
      party_reviewers: 3
  beta:
    "*":
      party_reviewers: 2
    api:
      channel: C0BETA00000
`

const slackOnlyDocument = `
mappings:
  acme:
    "*":
      channel: C0STAR00000
    the-monorepo:
      channel: C0BASE00000
      paths:
        services: {mentions: ["<@U0S>"]}
        docs: {channel: C0DOCS00000}
  beta:
    api:
      channel: C0BETA00000
`

func TestProvider_PartyOnlyEntriesLeaveSlackRoutingUnchanged(t *testing.T) {
	ctx := context.Background()
	slackProvider := providerFromDoc(t, slackOnlyDocument)
	partyProvider := providerFromDoc(t, partyDocument)
	repositories := []string{"acme/the-monorepo", "acme/web", "acme/other", "beta/api", "beta/other"}
	fileSets := [][]string{
		{"services/billing/x.go"},
		{"tools/run.sh"},
		{"docs/a.md", "README.md"},
		{"services/api/y.go", "services/billing/x.go"},
	}

	for _, repository := range repositories {
		t.Run(repository, func(t *testing.T) {
			wantMapping, wantErr := slackProvider.Get(ctx, repository)
			gotMapping, gotErr := partyProvider.Get(ctx, repository)
			assert.Equal(t, wantErr, gotErr)
			assert.Equal(t, wantMapping, gotMapping)
			assert.Equal(t, slackProvider.BaseTargets(repository), partyProvider.BaseTargets(repository))
			assert.Equal(t, slackProvider.RepoHasPathRules(repository), partyProvider.RepoHasPathRules(repository))
			assert.Equal(t, slackProvider.AdditionalChannels(repository), partyProvider.AdditionalChannels(repository))
			assert.Equal(t, slackProvider.DigestFor(repository), partyProvider.DigestFor(repository))
			for _, files := range fileSets {
				assert.Equal(t, slackProvider.TargetsForFiles(repository, files), partyProvider.TargetsForFiles(repository, files), "files %v", files)
			}
		})
	}

	assert.Equal(t, slackProvider.HasPathRules(), partyProvider.HasPathRules())
	assert.Equal(t, slackProvider.Schedules(), partyProvider.Schedules())
	assert.Equal(t, slackProvider.NotificationSchedules(), partyProvider.NotificationSchedules())

	slackEntries := slackProvider.Entries()
	partyEntries := partyProvider.Entries()
	require.Equal(t, slackEntries, partyEntries, "party keys must not add or change validation entries")
	for index := range slackEntries {
		assert.Equal(t, slackEntries[index].Hash(), partyEntries[index].Hash(), "config.lock hash of %s", slackEntries[index].Key())
	}

	_, err := partyProvider.Get(ctx, "beta/other")
	require.ErrorIs(t, err, domain.ErrNotFound, "a party-only star must route nothing")
}
