package infrastructure_test

import (
	"context"
	"fmt"
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
        services/ledger: {party_reviewers: null}
        docs: {channel: C0DOCS00000, party_reviewers: 1}
        tools: {party_deadline: 30m}
    web:
      party_reviewers: 3
  beta:
    "*":
      party_reviewers: 2
    api:
      channel: C0BETA00000
  gamma:
    "*":
      party_deadline: ""
    api:
      channel: C0GAMMA0000
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
  gamma:
    api:
      channel: C0GAMMA0000
`

func TestProvider_PartyOnlyEntriesLeaveSlackRoutingUnchanged(t *testing.T) {
	ctx := context.Background()
	slackProvider := providerFromDoc(t, slackOnlyDocument)
	partyProvider := providerFromDoc(t, partyDocument)
	repositories := []string{"acme/the-monorepo", "acme/web", "acme/other", "beta/api", "beta/other", "gamma/api", "gamma/other"}
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
}

func TestProvider_PartyKeyEditKeepsSlackTargets(t *testing.T) {
	const document = `
mappings:
  acme:
    api:
      channel: C0API000000
      mentions: ["<@U0BASE>"]
      paths:
        services: {mentions: ["<@U0S>"]}
        services/billing: %s
`
	files := []string{"services/billing/x.go"}
	withCap := providerFromDoc(t, fmt.Sprintf(document, "{party_reviewers: 4}")).TargetsForFiles("acme/api", files)
	testCases := []struct {
		name string
		rule string
	}{
		{name: "party key set to null", rule: "{party_reviewers: null}"},
		{name: "party key set to empty", rule: `{party_deadline: ""}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			targets := providerFromDoc(t, fmt.Sprintf(document, testCase.rule)).TargetsForFiles("acme/api", files)

			assert.Equal(t, withCap, targets, "changing a party key must not change a Slack notification")
		})
	}
}

func TestProvider_PartyOnlyStarRoutesNothing(t *testing.T) {
	partyProvider := providerFromDoc(t, partyDocument)

	_, err := partyProvider.Get(context.Background(), "beta/other")

	require.ErrorIs(t, err, domain.ErrNotFound, "a party-only star must route nothing")
}

func TestProvider_PartyOnlyPathRulesLeaveNoPathRouting(t *testing.T) {
	provider := providerFromDoc(t, `
mappings:
  acme:
    api:
      channel: C0API000000
      paths:
        billing: {party_reviewers: 2}
        tools: {party_deadline: 30m}
`)

	assert.False(t, provider.RepoHasPathRules("acme/api"))
	assert.False(t, provider.HasPathRules())
	assert.Equal(t, provider.BaseTargets("acme/api"), provider.TargetsForFiles("acme/api", []string{"billing/x.go"}))
}
