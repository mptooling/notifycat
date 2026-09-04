package infrastructure

import (
	"context"

	"github.com/mptooling/notifycat/internal/maintenance/domain"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
)

// RoutingPolicy adapts the routing provider to the relocate use case's config
// ports: which reaction emoji a repository's notifications use, and which
// channels it is currently configured to post to.
type RoutingPolicy struct {
	provider *routingapp.Provider
}

// NewRoutingPolicy wraps the routing provider.
func NewRoutingPolicy(provider *routingapp.Provider) *RoutingPolicy {
	return &RoutingPolicy{provider: provider}
}

// AllowedReactions implements domain.ReactionPolicy. The new-PR emoji is always
// in the set; the close and review emoji only when the repo enables reactions,
// mirroring the handlers that add them.
func (p *RoutingPolicy) AllowedReactions(ctx context.Context, repository string) ([]string, error) {
	mapping, err := p.provider.Get(ctx, repository)
	if err != nil {
		return nil, err
	}
	reactions := mapping.Reactions
	allowed := []string{reactions.NewPR}
	if reactions.Enabled {
		allowed = append(allowed,
			reactions.MergedPR,
			reactions.ClosedPR,
			reactions.Approved,
			reactions.Commented,
			reactions.RequestChange,
			reactions.BotReview,
		)
	}
	return nonEmpty(allowed), nil
}

// ConfiguredChannels implements domain.ChannelConfig: the repository's base
// fan-out plus every extra base-list and `paths:` channel.
func (p *RoutingPolicy) ConfiguredChannels(repository string) []string {
	var channels []string
	for _, target := range p.provider.BaseTargets(repository) {
		channels = append(channels, target.Channel)
	}
	channels = append(channels, p.provider.AdditionalChannels(repository)...)
	return nonEmpty(channels)
}

// nonEmpty drops blank entries, which stand for "this repo does not configure
// one" in both a reaction set and a resolved channel.
func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

var (
	_ domain.ReactionPolicy = (*RoutingPolicy)(nil)
	_ domain.ChannelConfig  = (*RoutingPolicy)(nil)
)
