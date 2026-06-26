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

	Reactions        *ReactionsOverride
	IgnoreAIReviews  *bool
	DependabotFormat *bool
	Digest           *DigestConfig
}

// ReactionsOverride is a tier's optional reaction overrides; each nil field
// inherits from a less-specific tier (org/* then the global config.yaml set).
type ReactionsOverride struct {
	Enabled       *bool
	NewPR         *string
	MergedPR      *string
	ClosedPR      *string
	Approved      *string
	Commented     *string
	RequestChange *string
	BotReview     *string
}

// UnmarshalYAML walks the reactions mapping by hand so unknown keys are
// rejected and every leaf is optional (nil = inherit).
func (r *ReactionsOverride) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("reactions: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("reactions: malformed mapping")
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		var dst any
		switch key.Value {
		case "enabled":
			r.Enabled = new(bool)
			dst = r.Enabled
		case "new_pr":
			r.NewPR = new(string)
			dst = r.NewPR
		case "merged_pr":
			r.MergedPR = new(string)
			dst = r.MergedPR
		case "closed_pr":
			r.ClosedPR = new(string)
			dst = r.ClosedPR
		case "approved":
			r.Approved = new(string)
			dst = r.Approved
		case "commented":
			r.Commented = new(string)
			dst = r.Commented
		case "request_change":
			r.RequestChange = new(string)
			dst = r.RequestChange
		case "bot_review":
			r.BotReview = new(string)
			dst = r.BotReview
		default:
			return fmt.Errorf("reactions: unknown field %q", key.Value)
		}
		if err := val.Decode(dst); err != nil {
			return fmt.Errorf("reactions.%s: %w", key.Value, err)
		}
	}
	return nil
}

// decodeReviews parses a tier's `reviews:` block (ignore_ai_reviews,
// dependabot_format), each optional, rejecting unknown keys.
func decodeReviews(rc *RepoConfig, node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("reviews: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("reviews: malformed mapping")
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		var dst *bool
		switch key.Value {
		case "ignore_ai_reviews":
			rc.IgnoreAIReviews = new(bool)
			dst = rc.IgnoreAIReviews
		case "dependabot_format":
			rc.DependabotFormat = new(bool)
			dst = rc.DependabotFormat
		default:
			return fmt.Errorf("reviews: unknown field %q", key.Value)
		}
		if err := val.Decode(dst); err != nil {
			return fmt.Errorf("reviews.%s: %w", key.Value, err)
		}
	}
	return nil
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
		case "reactions":
			r := &ReactionsOverride{}
			if err := valNode.Decode(r); err != nil {
				return fmt.Errorf("reactions: %w", err)
			}
			rc.Reactions = r
		case "reviews":
			if err := decodeReviews(rc, valNode); err != nil {
				return err
			}
		case "digest":
			d := &DigestConfig{}
			if err := valNode.Decode(d); err != nil {
				return fmt.Errorf("digest: %w", err)
			}
			rc.Digest = d
		default:
			return fmt.Errorf("unknown field %q", keyNode.Value)
		}
	}
	return nil
}

func isNullNode(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || n.Value == "null" || n.Value == "~")
}
