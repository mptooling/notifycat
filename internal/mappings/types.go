package mappings

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ChannelMention is the Slack wire token used when an entry omits the
// `mentions:` key — operators see `@channel` in Slack.
const ChannelMention = "<!channel>"

// File is the parsed mappings.yaml document.
type File struct {
	Mappings map[string]Org `yaml:"mappings"`
}

// Org is one organization's mapping: every configured repository in the org
// shares this channel and mentions list. MentionsPresent distinguishes the
// absent-key case (fall back to @channel at lookup time) from `mentions: []`
// (ping nobody).
type Org struct {
	Channel         string
	Mentions        []string
	MentionsPresent bool
	Repositories    Repositories
}

// UnmarshalYAML walks the mapping node by hand so we can:
//   - distinguish a missing `mentions:` key from `mentions: []`,
//   - reject explicit `mentions: null` (ambiguous; operators should remove
//     the key or use `[]`),
//   - keep KnownFields-style rejection of unknown keys.
func (o *Org) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("org: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("org: malformed mapping")
	}
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("org: non-scalar key")
		}
		switch keyNode.Value {
		case "channel":
			if err := valNode.Decode(&o.Channel); err != nil {
				return fmt.Errorf("channel: %w", err)
			}
		case "mentions":
			o.MentionsPresent = true
			if isNullNode(valNode) {
				return fmt.Errorf("mentions: null is not allowed; omit the key for @channel or use [] for none")
			}
			if valNode.Kind != yaml.SequenceNode {
				return fmt.Errorf("mentions: must be a list (use [] for none, omit the key for @channel)")
			}
			ms := []string{}
			if err := valNode.Decode(&ms); err != nil {
				return fmt.Errorf("mentions: %w", err)
			}
			o.Mentions = ms
		case "repositories":
			if err := valNode.Decode(&o.Repositories); err != nil {
				return fmt.Errorf("repositories: %w", err)
			}
		default:
			return fmt.Errorf("unknown field %q", keyNode.Value)
		}
	}
	return nil
}

func isNullNode(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || n.Value == "null" || n.Value == "~")
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
