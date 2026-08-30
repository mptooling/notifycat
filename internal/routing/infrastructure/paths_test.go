package infrastructure_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const baseRepoHead = "mappings:\n  acme:\n    the-monorepo:\n      channel: C0BASE0000\n"

func TestPaths_ParsedInDeclarationOrder(t *testing.T) {
	file, err := parseMappings(baseRepoHead +
		"      paths:\n" +
		"        \"/modules/acme\": {mentions: [\"<@U1>\"]}\n" +
		"        \"/src/AuthBundle/\": {channel: C0AUTH0000, mentions: [\"<@U2>\"]}\n" +
		"        \"config\": {mentions: []}\n")

	require.NoError(t, err)
	paths := file.Mappings["acme"]["the-monorepo"].Paths
	require.Len(t, paths, 3)

	dirs := []string{paths[0].Dir, paths[1].Dir, paths[2].Dir}
	assert.Equal(t, []string{"modules/acme", "src/AuthBundle", "config"}, dirs, "keys are normalized and kept in declaration order")

	assert.True(t, paths[0].MentionsPresent)
	assert.Equal(t, []string{"<@U1>"}, paths[0].Mentions)
	assert.Equal(t, "C0AUTH0000", paths[1].Channel)
	assert.True(t, paths[2].MentionsPresent)
	assert.Empty(t, paths[2].Mentions, "mentions: [] is present-and-empty, not absent")
}

func TestPaths_AbsentMentionsNotPresent(t *testing.T) {
	file, err := parseMappings(baseRepoHead + "      paths:\n        \"/legacy\": {channel: C0LEG00000}\n")

	require.NoError(t, err)
	assert.False(t, file.Mappings["acme"]["the-monorepo"].Paths[0].MentionsPresent, "absent mentions inherit")
}

func TestPaths_DuplicateChannelInTierRejected(t *testing.T) {
	_, err := parseMappings("mappings:\n  acme:\n    the-monorepo:\n      channel: C0AAA00000\n      channel: C0BBB00000\n")

	require.Error(t, err, "a duplicate key must fail rather than silently last-win")
}

func TestPaths_DuplicateKeyInPathNodeRejected(t *testing.T) {
	_, err := parseMappings(baseRepoHead +
		"      paths:\n        \"/src\": {channel: C0AAA00000, channel: C0BBB00000}\n")

	require.Error(t, err)
}

func TestPaths_NormalizationCollisionRejected(t *testing.T) {
	_, err := parseMappings(baseRepoHead +
		"      paths:\n        \"/config\": {channel: C0AAA00000}\n        \"config/\": {mentions: []}\n")

	require.Error(t, err, "/config and config/ normalize to the same directory")
}

func TestPaths_RootKeyRejected(t *testing.T) {
	_, err := parseMappings(baseRepoHead + "      paths:\n        \"/\": {channel: C0AAA00000}\n")

	require.Error(t, err)
}

func TestPaths_DotDotKeyRejected(t *testing.T) {
	_, err := parseMappings(baseRepoHead + "      paths:\n        \"/src/../etc\": {channel: C0AAA00000}\n")

	require.Error(t, err)
}

func TestPaths_OnStarTierRejected(t *testing.T) {
	_, err := parseMappings("mappings:\n  acme:\n    \"*\":\n      channel: C0STAR0000\n      paths:\n        \"/src\": {mentions: []}\n")

	require.Error(t, err, "paths belong to a concrete repo, never the org default tier")
}

func TestPaths_InvalidChannelFormatRejected(t *testing.T) {
	_, err := parseMappings(baseRepoHead + "      paths:\n        \"/src\": {channel: \"not-a-channel\"}\n")

	require.Error(t, err)
}

func TestPaths_UnknownKeyInPathRejected(t *testing.T) {
	_, err := parseMappings(baseRepoHead + "      paths:\n        \"/src\": {bogus: x}\n")

	require.Error(t, err)
}

func TestPaths_NullMentionsRejected(t *testing.T) {
	_, err := parseMappings(baseRepoHead + "      paths:\n        \"/src\": {mentions: null}\n")

	require.Error(t, err)
}

func TestPaths_NoBaseChannelWithPathsRejected(t *testing.T) {
	_, err := parseMappings("mappings:\n  acme:\n    the-monorepo:\n      paths:\n        \"/src\": {channel: C0AAA00000}\n")

	require.Error(t, err, "a repo with paths still needs a base channel or an org/* to inherit")
}
