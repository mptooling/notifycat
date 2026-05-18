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
	for org, o := range f.Mappings {
		if !orgPattern.MatchString(org) {
			return fmt.Errorf("mappings: org %q: invalid name (must match %s)", org, orgPattern)
		}
		if !channelPattern.MatchString(o.Channel) {
			return fmt.Errorf("mappings: org %q: invalid channel %q", org, o.Channel)
		}
		if !o.Repositories.All {
			seen := make(map[string]struct{}, len(o.Repositories.List))
			for _, repo := range o.Repositories.List {
				if !repoPattern.MatchString(repo) {
					return fmt.Errorf("mappings: org %q: invalid repository %q", org, repo)
				}
				if _, dup := seen[repo]; dup {
					return fmt.Errorf("mappings: org %q: duplicate repository %q", org, repo)
				}
				seen[repo] = struct{}{}
			}
		}
	}
	return nil
}
