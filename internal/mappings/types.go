package mappings

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// File is the parsed mappings.yaml document.
type File struct {
	Mappings map[string]Org `yaml:"mappings"`
}

// Org is one organization's mapping: every configured repository in the org
// shares this channel and mentions list.
type Org struct {
	Channel      string       `yaml:"channel"`
	Mentions     []string     `yaml:"mentions"`
	Repositories Repositories `yaml:"repositories"`
}

// Repositories is "*" (whole org) XOR a non-empty list of bare repo names.
// The YAML accepts either shape; the in-memory representation normalizes.
type Repositories struct {
	All  bool
	List []string
}

// UnmarshalYAML decodes either the wildcard string "*" or a list of repo
// names. "*" inside a list, an empty list, or any other shape is rejected.
func (r *Repositories) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value != "*" {
			return fmt.Errorf("repositories: scalar must be \"*\"; got %q", node.Value)
		}
		r.All = true
		return nil
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return fmt.Errorf("repositories: list cannot be empty (use \"*\" for whole-org)")
		}
		items := make([]string, 0, len(node.Content))
		for _, c := range node.Content {
			if c.Kind != yaml.ScalarNode {
				return fmt.Errorf("repositories: list entry must be a string")
			}
			if c.Value == "*" {
				return fmt.Errorf("repositories: \"*\" cannot appear inside a list (use \"*\" alone)")
			}
			items = append(items, c.Value)
		}
		r.List = items
		return nil
	default:
		return fmt.Errorf("repositories: expected scalar \"*\" or a list; got node kind %d", node.Kind)
	}
}
