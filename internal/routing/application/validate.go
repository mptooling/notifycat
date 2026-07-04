package application

import (
	"fmt"
	"regexp"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

var (
	orgPattern     = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	repoPattern    = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	channelPattern = regexp.MustCompile(`^[CGD][A-Z0-9]{2,}$`)
)

// ValidateMappings runs the same per-org/per-tier structural checks as Parse
// over a mappings map. An empty map (no orgs) is valid. Returns an error for
// invalid org names, empty orgs, bad repo keys, malformed channel IDs, or any
// repo tier that cannot resolve a channel.
func ValidateMappings(m map[string]domain.Org) error {
	for org, o := range m {
		if !orgPattern.MatchString(org) {
			return fmt.Errorf("mappings: org %q: invalid name (must match %s)", org, orgPattern)
		}
		if len(o) == 0 {
			return fmt.Errorf("mappings: org %q: has no repo entries", org)
		}
		star, hasStar := o[domain.WildcardKey]
		starHasChannel := hasStar && star.Channel != ""
		for repo, rc := range o {
			if repo != domain.WildcardKey && !repoPattern.MatchString(repo) {
				return fmt.Errorf("mappings: org %q: invalid repo key %q (use a bare repo name or \"*\")", org, repo)
			}
			if rc.Channel != "" && !channelPattern.MatchString(rc.Channel) {
				return fmt.Errorf("mappings: org %q repo %q: invalid channel %q", org, repo, rc.Channel)
			}
			// Every resolvable path must yield a channel: this tier sets one,
			// or org/* supplies it.
			if rc.Channel == "" && !starHasChannel {
				return fmt.Errorf("mappings: org %q repo %q: no channel (set channel here or in the org's \"*\" entry)", org, repo)
			}
			if err := validatePaths(org, repo, rc.Paths); err != nil {
				return err
			}
		}
	}
	return nil
}

// validatePaths enforces the per-path constraints that need tier context:
// `paths:` is rejected on the "*" org-default tier (it would apply to every
// repo in the org), and any path channel must be a well-formed Slack ID. Path
// channels are optional (they inherit the repo/org channel); only a set one is
// format-checked.
func validatePaths(org, repo string, paths []domain.PathRule) error {
	if len(paths) == 0 {
		return nil
	}
	if repo == domain.WildcardKey {
		return fmt.Errorf("mappings: org %q: paths are not allowed on the \"*\" tier (set them on a named repo)", org)
	}
	for _, p := range paths {
		if p.Channel != "" && !channelPattern.MatchString(p.Channel) {
			return fmt.Errorf("mappings: org %q repo %q path %q: invalid channel %q", org, repo, p.Dir, p.Channel)
		}
	}
	return nil
}
