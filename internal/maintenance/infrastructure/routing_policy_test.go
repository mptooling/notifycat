package infrastructure

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

func newPolicy(orgs map[string]routingdomain.Org, defaults routingdomain.Defaults) *RoutingPolicy {
	return NewRoutingPolicy(routingapp.NewProvider(defaults, orgs, nil))
}

func TestRoutingPolicy_ConfiguredChannels_CoversBaseListAndPaths(t *testing.T) {
	policy := newPolicy(map[string]routingdomain.Org{
		"acme": {
			"mono": {
				Channel: "C_BASE",
				Paths: []routingdomain.PathRule{
					{Dir: "auth", Channel: "C_AUTH"},
					{Dir: "billing", Channels: []routingdomain.ChannelSpec{{Channel: "C_BILL"}}},
				},
			},
		},
	}, routingdomain.Defaults{})

	channels := policy.ConfiguredChannels("acme/mono")

	assert.ElementsMatch(t, []string{"C_BASE", "C_AUTH", "C_BILL"}, channels)
}

func TestRoutingPolicy_ConfiguredChannels_UnmappedRepoHasNone(t *testing.T) {
	policy := newPolicy(map[string]routingdomain.Org{}, routingdomain.Defaults{})

	assert.Empty(t, policy.ConfiguredChannels("ghost/unmapped"))
}

func TestRoutingPolicy_AllowedReactions_ReviewEmojiOnlyWhenEnabled(t *testing.T) {
	reactions := routingdomain.Reactions{
		Enabled: true, NewPR: "new", MergedPR: "merged", ClosedPR: "closed",
		Approved: "white_check_mark", Commented: "speech_balloon", RequestChange: "warning",
	}
	policy := newPolicy(map[string]routingdomain.Org{
		"acme": {"api": {Channel: "C_BASE"}},
	}, routingdomain.Defaults{Reactions: reactions})

	allowed, err := policy.AllowedReactions(context.Background(), "acme/api")

	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{"new", "merged", "closed", "white_check_mark", "speech_balloon", "warning"},
		allowed, "an empty bot-review emoji contributes nothing")
}

func TestRoutingPolicy_AllowedReactions_DisabledKeepsOnlyNewPR(t *testing.T) {
	policy := newPolicy(map[string]routingdomain.Org{
		"acme": {"api": {Channel: "C_BASE"}},
	}, routingdomain.Defaults{Reactions: routingdomain.Reactions{NewPR: "new", Approved: "white_check_mark"}})

	allowed, err := policy.AllowedReactions(context.Background(), "acme/api")

	require.NoError(t, err)
	assert.Equal(t, []string{"new"}, allowed)
}
