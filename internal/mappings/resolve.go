package mappings

import (
	"github.com/mptooling/notifycat/internal/store"
)

// Resolved is the effective routing config for one repository after merging
// the org/repo tier over the org/* tier.
type Resolved struct {
	Channel  string
	Mentions []string
}

// resolveRouting merges the wildcard (org/*) tier under the specific
// (org/repo) tier. At least one of star/repo must be non-nil. For each key,
// the most specific tier that sets it wins; an absent mentions key inherits,
// falling back to @channel only when no tier set mentions.
func resolveRouting(star, repo *RepoConfig) Resolved {
	var r Resolved
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
		r.Mentions = []string{ChannelMention}
	}
	return r
}

// Defaults is the global tier: the config.yaml top-level behavioral settings
// that per-repo tiers override.
type Defaults struct {
	Reactions        store.Reactions
	IgnoreAIReviews  bool
	DependabotFormat bool
}

// resolveBehavior merges the global, org/*, and org/repo tiers for the
// behavioral keys. For each key the most specific tier that set it wins; the
// global value is the base. star/repo may be nil.
func resolveBehavior(global Defaults, star, repo *RepoConfig) (store.Reactions, bool, bool) {
	rx := global.Reactions
	ignoreAI := global.IgnoreAIReviews
	dependabot := global.DependabotFormat

	apply := func(rc *RepoConfig) {
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
