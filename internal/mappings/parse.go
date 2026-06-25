package mappings

import (
	"fmt"
	"io"
	"regexp"

	"gopkg.in/yaml.v3"
)

var (
	orgPattern     = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	repoPattern    = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	channelPattern = regexp.MustCompile(`^[CGD][A-Z0-9]{2,}$`)
)

const starKey = "*"

// Parse reads + validates the YAML document. Unknown keys and shape errors
// are returned as errors (the server fails fast at startup).
//
// `mentions:` is optional: an absent key means "ping @channel"; `mentions: []`
// means "ping nobody"; `mentions: null` is rejected (ambiguous).
func Parse(r io.Reader) (File, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var f File
	if err := dec.Decode(&f); err != nil {
		return File{}, fmt.Errorf("mappings: parse: %w", err)
	}
	if err := f.validate(); err != nil {
		return File{}, err
	}
	return f, nil
}

func (f File) validate() error {
	return ValidateMappings(f.Mappings)
}

// ValidateMappings runs the same per-org/per-tier structural checks as Parse
// over a mappings map. An empty map (no orgs) is valid. Returns an error for
// invalid org names, empty orgs, bad repo keys, malformed channel IDs, or any
// repo tier that cannot resolve a channel.
func ValidateMappings(m map[string]Org) error {
	for org, o := range m {
		if !orgPattern.MatchString(org) {
			return fmt.Errorf("mappings: org %q: invalid name (must match %s)", org, orgPattern)
		}
		if len(o) == 0 {
			return fmt.Errorf("mappings: org %q: has no repo entries", org)
		}
		star, hasStar := o[starKey]
		starHasChannel := hasStar && star.Channel != ""
		for repo, rc := range o {
			if repo != starKey && !repoPattern.MatchString(repo) {
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
		}
	}
	return nil
}
