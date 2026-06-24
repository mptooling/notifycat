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
	Digest   *DigestConfig  `yaml:"digest"`
	Mappings map[string]Org `yaml:"mappings"`
}

// DigestConfig is the optional global `digest:` section: a scheduled reminder
// that lists open PRs nobody has touched since the previous day. It is a
// global parameter — one schedule for every org/repo, not per-entry. The
// section is optional and the feature is on by default, so an absent section
// behaves like `{enabled: true}` with the default schedule.
type DigestConfig struct {
	Enabled  bool
	Schedule string
}

// UnmarshalYAML walks the mapping node by hand (like Org) so we can default
// Enabled to true — distinguishing a missing `enabled:` key from an explicit
// `enabled: false` — while keeping KnownFields-style rejection of typos.
func (d *DigestConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("digest: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("digest: malformed mapping")
	}
	d.Enabled = true // on by default; an explicit `enabled: false` overrides
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("digest: non-scalar key")
		}
		switch keyNode.Value {
		case "enabled":
			if err := valNode.Decode(&d.Enabled); err != nil {
				return fmt.Errorf("digest: enabled: %w", err)
			}
		case "schedule":
			if err := valNode.Decode(&d.Schedule); err != nil {
				return fmt.Errorf("digest: schedule: %w", err)
			}
		default:
			return fmt.Errorf("digest: unknown field %q", keyNode.Value)
		}
	}
	return nil
}

// Org maps each repo name (or the literal "*") to its tier config. The "*"
// key is the org-level tier: it both supplies defaults that explicit repo
// tiers inherit and matches any repo not named explicitly.
type Org map[string]RepoConfig

// RepoConfig is one tier (org/repo or org/*). Phase A carries routing only.
// An empty Channel means this tier does not set a channel (it inherits).
// MentionsPresent distinguishes an absent mentions key (inherit) from an
// explicit empty list (ping nobody).
type RepoConfig struct {
	Channel         string
	Mentions        []string
	MentionsPresent bool
}

// UnmarshalYAML walks the mapping node by hand so we can keep the mentions
// tri-state (absent vs [] vs null) and reject unknown keys, mirroring the
// 0.17 Org decoder but at the per-repo tier level.
func (rc *RepoConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("repo config: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("repo config: malformed mapping")
	}
	for i := 0; i < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("repo config: non-scalar key")
		}
		switch keyNode.Value {
		case "channel":
			if err := valNode.Decode(&rc.Channel); err != nil {
				return fmt.Errorf("channel: %w", err)
			}
		case "mentions":
			rc.MentionsPresent = true
			if isNullNode(valNode) {
				return fmt.Errorf("mentions: null is not allowed; omit the key to inherit/@channel or use [] for none")
			}
			if valNode.Kind != yaml.SequenceNode {
				return fmt.Errorf("mentions: must be a list (use [] for none, omit the key to inherit)")
			}
			ms := []string{}
			if err := valNode.Decode(&ms); err != nil {
				return fmt.Errorf("mentions: %w", err)
			}
			rc.Mentions = ms
		default:
			return fmt.Errorf("unknown field %q", keyNode.Value)
		}
	}
	return nil
}

func isNullNode(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || n.Value == "null" || n.Value == "~")
}
