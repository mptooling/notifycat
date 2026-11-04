package infrastructure_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
	infrastructure "github.com/mptooling/notifycat/internal/routing/infrastructure"
)

func parseMappings(body string) (domain.File, error) {
	return infrastructure.Parse(strings.NewReader(body))
}

func TestParse_PerRepoTiers_OK(t *testing.T) {
	file, err := parseMappings(`
mappings:
  acme:
    api:
      channel: C0API
    web:
      channel: C0WEB
    "*":
      channel: C0DEFAULT
`)

	require.NoError(t, err)
	assert.Equal(t, "C0API", file.Mappings["acme"]["api"].Channel)
	assert.Equal(t, "C0WEB", file.Mappings["acme"]["web"].Channel)
	assert.Equal(t, "C0DEFAULT", file.Mappings["acme"]["*"].Channel)
}

func TestParse_InheritsChannelFromStar(t *testing.T) {
	_, err := parseMappings(`
mappings:
  acme:
    api:
      mentions: ["<@U1>"]
    "*":
      channel: C0DEFAULT
`)

	require.NoError(t, err, "api carries no channel but inherits org/* at resolve time")
}

func TestParse_RepoWithoutChannelAndNoStarRejected(t *testing.T) {
	_, err := parseMappings(`
mappings:
  acme:
    api:
      mentions: ["<@U1>"]
`)

	require.Error(t, err, "api has no channel and no org/* to inherit from")
}

func TestParse_BadChannelRejected(t *testing.T) {
	_, err := parseMappings("mappings:\n  acme:\n    api:\n      channel: not-a-channel\n")

	require.Error(t, err)
}

func TestParse_BadRepoKeyRejected(t *testing.T) {
	_, err := parseMappings("mappings:\n  acme:\n    \"a/b\":\n      channel: C0API\n")

	require.Error(t, err, "a repo key must not contain a slash")
}

func TestParse_EmptyOrgRejected(t *testing.T) {
	_, err := parseMappings("mappings:\n  acme: {}\n")

	require.Error(t, err)
}

func TestParse_ListChannelInvalidID(t *testing.T) {
	_, err := parseMappings("mappings:\n  acme:\n    api:\n      channels:\n        - channel: not-a-channel\n")

	require.Error(t, err)
}

func TestParse_ListSatisfiesChannelRequirement(t *testing.T) {
	_, err := parseMappings("mappings:\n  acme:\n    api:\n      channels:\n        - channel: C0API1\n")

	require.NoError(t, err, "a channels: list stands in for channel:")
}

func TestParse_PathListChannelInvalidID(t *testing.T) {
	_, err := parseMappings("mappings:\n  acme:\n    monorepo:\n      channel: C0BASE\n      paths:\n        services/pay:\n          channels:\n            - channel: bad\n")

	require.Error(t, err)
}

func TestParse_PartyOnlyTierUnderRoutingStar(t *testing.T) {
	_, err := parseMappings(`
mappings:
  acme:
    "*":
      channel: C0STAR00000
    web:
      party_reviewers: 3
`)

	require.NoError(t, err)
}

func TestParse_PartyOnlyTierWithoutRoutingStarRejected(t *testing.T) {
	_, err := parseMappings(`
mappings:
  acme:
    api:
      channel: C0API000000
    web:
      party_reviewers: 3
`)

	require.ErrorContains(t, err, `mappings: org "acme" repo "web": sets only party keys, but no tier routes this repository`)
}

func TestParse_PartyOnlyStarRoutesNothingRejected(t *testing.T) {
	_, err := parseMappings(`
mappings:
  acme:
    "*":
      party_reviewers: 2
`)

	require.ErrorContains(t, err, `mappings: org "acme": the "*" entry sets only party keys, but no tier of the org routes a repository`)
}

func TestParse_PartyOnlyStarWithRoutedRepository(t *testing.T) {
	_, err := parseMappings(`
mappings:
  acme:
    "*":
      party_deadline: 2h
    api:
      channel: C0API000000
`)

	require.NoError(t, err)
}

func TestParse_PartyPathOnStarRejected(t *testing.T) {
	_, err := parseMappings(`
mappings:
  acme:
    "*":
      paths:
        billing: {party_reviewers: 2}
    api:
      channel: C0API000000
`)

	require.ErrorContains(t, err, `mappings: org "acme": paths are not allowed on the "*" tier`)
}
