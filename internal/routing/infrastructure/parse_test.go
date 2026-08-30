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
