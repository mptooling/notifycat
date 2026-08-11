package application

import (
	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

// resolveBaseTargets resolves a tier's base fan-out: the channels a PR is always
// announced to. The most-specific tier that declares a channel (repo, else star),
// in either form, wins wholesale — lists are not merged across tiers. Always
// returns at least one target (an empty-channel target when no tier sets one,
// preserving the single-form behavior).
func resolveBaseTargets(star, repo *domain.RepoConfig) []domain.Target {
	if repo != nil && len(repo.Channels) > 0 {
		return listTargets(repo.Channels)
	}
	if repo != nil && repo.Channel != "" {
		return []domain.Target{{Channel: repo.Channel, Mentions: resolveMentions(star, repo)}}
	}
	if star != nil && len(star.Channels) > 0 {
		return listTargets(star.Channels)
	}
	if star != nil && star.Channel != "" {
		return []domain.Target{{Channel: star.Channel, Mentions: resolveMentions(star, repo)}}
	}
	return []domain.Target{{Channel: "", Mentions: resolveMentions(star, repo)}}
}

// listTargets expands a channels: list into targets, defaulting an absent
// mentions key to ChannelMention (list form has no cross-tier inheritance).
func listTargets(specs []domain.ChannelSpec) []domain.Target {
	out := make([]domain.Target, 0, len(specs))
	for _, spec := range specs {
		out = append(out, domain.Target{Channel: spec.Channel, Mentions: specMentions(spec)})
	}
	return out
}

// specMentions resolves one list entry's mentions: explicit list/[] as given,
// absent key → ChannelMention.
func specMentions(spec domain.ChannelSpec) []string {
	if spec.MentionsPresent {
		return append([]string(nil), spec.Mentions...)
	}
	return []string{domain.ChannelMention}
}

// resolveMentions is the single-form cross-tier tri-state: the most-specific
// tier that set a mentions key wins; absent everywhere falls back to ChannelMention.
func resolveMentions(star, repo *domain.RepoConfig) []string {
	switch {
	case repo != nil && repo.MentionsPresent:
		return append([]string(nil), repo.Mentions...)
	case star != nil && star.MentionsPresent:
		return append([]string(nil), star.Mentions...)
	default:
		return []string{domain.ChannelMention}
	}
}

// resolveRouting returns the primary base target (channel + mentions) for a
// tier: the first of resolveBaseTargets. Consumers that need only one channel
// (RepoMapping.SlackChannel, Entry.Channel) use this.
func resolveRouting(star, repo *domain.RepoConfig) domain.Resolved {
	primary := resolveBaseTargets(star, repo)[0]
	return domain.Resolved{Channel: primary.Channel, Mentions: primary.Mentions}
}

// resolveBehavior merges the global, org/*, and org/repo tiers for the
// behavioral keys. For each key the most specific tier that set it wins; the
// global value is the base. star/repo may be nil.
func resolveBehavior(global domain.Defaults, star, repo *domain.RepoConfig) (domain.Reactions, bool, bool) {
	rx := global.Reactions
	ignoreAI := global.IgnoreAIReviews
	dependabot := global.DependabotFormat

	apply := func(rc *domain.RepoConfig) {
		if rc == nil {
			return
		}
		if o := rc.Reactions; o != nil {
			if o.Enabled != nil {
				rx.Enabled = *o.Enabled
			}
			setStr(&rx.NewPR, o.NewPR)
			setStr(&rx.MergedPR, o.MergedPR)
			setStr(&rx.ClosedPR, o.ClosedPR)
			setStr(&rx.Approved, o.Approved)
			setStr(&rx.Commented, o.Commented)
			setStr(&rx.RequestChange, o.RequestChange)
			setStr(&rx.BotReview, o.BotReview)
		}
		if rc.IgnoreAIReviews != nil {
			ignoreAI = *rc.IgnoreAIReviews
		}
		if rc.DependabotFormat != nil {
			dependabot = *rc.DependabotFormat
		}
	}
	apply(star)
	apply(repo)
	return rx, ignoreAI, dependabot
}

func setStr(dst *string, v *string) {
	if v != nil {
		*dst = *v
	}
}
