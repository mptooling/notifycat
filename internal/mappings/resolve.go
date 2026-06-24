package mappings

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
