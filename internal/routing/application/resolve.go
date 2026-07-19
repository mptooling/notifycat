package application

import (
	"strings"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

// resolveRouting merges the wildcard (org/*) tier under the specific
// (org/repo) tier. At least one of star/repo must be non-nil. For each key,
// the most specific tier that sets it wins; an absent mentions key inherits,
// falling back to @channel only when no tier set mentions.
func resolveRouting(star, repo *domain.RepoConfig) domain.Resolved {
	var r domain.Resolved
	if repo != nil && repo.Channel != "" {
		r.Channel = repo.Channel
	} else if star != nil && star.Channel != "" {
		r.Channel = star.Channel
	}
	switch {
	case repo != nil && repo.MentionsPresent:
		r.Mentions = append([]string(nil), repo.Mentions...)
	case star != nil && star.MentionsPresent:
		r.Mentions = append([]string(nil), star.Mentions...)
	default:
		r.Mentions = []string{domain.ChannelMention}
	}
	return r
}

// behaviorResolution is the merged behavioral config across the global,
// org/*, and org/repo tiers.
type behaviorResolution struct {
	reactions        domain.Reactions
	ignoreAIReviews  bool
	dependabotFormat bool
	aiEnabled        bool
	aiInstructions   string
}

// resolveBehavior merges the global, org/*, and org/repo tiers for the
// behavioral keys. For each key the most specific tier that set it wins,
// except ai instructions, which concatenate so guidance narrows rather than
// replaces. star/repo may be nil.
func resolveBehavior(global domain.Defaults, star, repo *domain.RepoConfig) behaviorResolution {
	resolution := behaviorResolution{
		reactions:        global.Reactions,
		ignoreAIReviews:  global.IgnoreAIReviews,
		dependabotFormat: global.DependabotFormat,
		aiEnabled:        global.AIEnabled,
		aiInstructions:   global.AIInstructions,
	}
	apply := func(repoConfig *domain.RepoConfig) {
		if repoConfig == nil {
			return
		}
		if o := repoConfig.Reactions; o != nil {
			if o.Enabled != nil {
				resolution.reactions.Enabled = *o.Enabled
			}
			setStr(&resolution.reactions.NewPR, o.NewPR)
			setStr(&resolution.reactions.MergedPR, o.MergedPR)
			setStr(&resolution.reactions.ClosedPR, o.ClosedPR)
			setStr(&resolution.reactions.Approved, o.Approved)
			setStr(&resolution.reactions.Commented, o.Commented)
			setStr(&resolution.reactions.RequestChange, o.RequestChange)
			setStr(&resolution.reactions.BotReview, o.BotReview)
		}
		if repoConfig.IgnoreAIReviews != nil {
			resolution.ignoreAIReviews = *repoConfig.IgnoreAIReviews
		}
		if repoConfig.DependabotFormat != nil {
			resolution.dependabotFormat = *repoConfig.DependabotFormat
		}
		if repoConfig.AI != nil {
			if repoConfig.AI.Enabled != nil {
				resolution.aiEnabled = *repoConfig.AI.Enabled
			}
			resolution.aiInstructions = joinInstructions(resolution.aiInstructions, repoConfig.AI.Instructions)
		}
	}
	apply(star)
	apply(repo)
	return resolution
}

// joinInstructions concatenates tier guidance blank-line separated, skipping
// empties.
func joinInstructions(base, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return base
	}
	if base == "" {
		return extra
	}
	return base + "\n\n" + extra
}

func setStr(dst *string, v *string) {
	if v != nil {
		*dst = *v
	}
}
