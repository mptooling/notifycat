package infrastructure

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

// digestConfigWire is the YAML wire type for the `digest:` section. It is
// decoded by hand so Enabled defaults to true when the key is absent.
type digestConfigWire struct {
	Enabled  bool
	Schedule string
	Timezone string
}

// UnmarshalYAML walks the mapping node by hand (like Org) so we can default
// Enabled to true — distinguishing a missing `enabled:` key from an explicit
// `enabled: false` — while keeping KnownFields-style rejection of typos.
func (d *digestConfigWire) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("digest: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("digest: malformed mapping")
	}
	d.Enabled = true // on by default; an explicit `enabled: false` overrides
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("digest: non-scalar key")
		}
		if err := markSeen(seen, keyNode.Value); err != nil {
			return fmt.Errorf("digest: %w", err)
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
		case "timezone":
			if err := valNode.Decode(&d.Timezone); err != nil {
				return fmt.Errorf("digest: timezone: %w", err)
			}
		default:
			return fmt.Errorf("digest: unknown field %q", keyNode.Value)
		}
	}
	return nil
}

// reactionsOverrideWire is the YAML wire type for a tier's `reactions:` block.
type reactionsOverrideWire struct {
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
func (r *reactionsOverrideWire) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("reactions: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("reactions: malformed mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		if err := markSeen(seen, key.Value); err != nil {
			return fmt.Errorf("reactions: %w", err)
		}
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

// repoConfigWire is the YAML wire type for one repo tier.
type repoConfigWire struct {
	Channel          string
	Mentions         []string
	MentionsPresent  bool
	Reactions        *reactionsOverrideWire
	IgnoreAIReviews  *bool
	DependabotFormat *bool
	Digest           *digestConfigWire
	Paths            []domain.PathRule
}

// UnmarshalYAML walks the mapping node by hand so we can keep the mentions
// tri-state (absent vs [] vs null) and reject unknown keys, mirroring the
// 0.17 Org decoder but at the per-repo tier level.
func (rc *repoConfigWire) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("repo config: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("repo config: malformed mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("repo config: non-scalar key")
		}
		if err := markSeen(seen, keyNode.Value); err != nil {
			return err
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
			r := &reactionsOverrideWire{}
			if err := valNode.Decode(r); err != nil {
				return fmt.Errorf("reactions: %w", err)
			}
			rc.Reactions = r
		case "reviews":
			if err := decodeReviews(rc, valNode); err != nil {
				return err
			}
		case "digest":
			d := &digestConfigWire{}
			if err := valNode.Decode(d); err != nil {
				return fmt.Errorf("digest: %w", err)
			}
			if d.Timezone != "" {
				return fmt.Errorf("digest: timezone is only valid in the global digest section, not per-repo")
			}
			rc.Digest = d
		case "paths":
			paths, err := decodePaths(valNode)
			if err != nil {
				return err
			}
			rc.Paths = paths
		default:
			return fmt.Errorf("unknown field %q", keyNode.Value)
		}
	}
	return nil
}

// decodeReviews parses a tier's `reviews:` block (ignore_ai_reviews,
// dependabot_format), each optional, rejecting unknown keys.
func decodeReviews(rc *repoConfigWire, node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("reviews: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("reviews: malformed mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		if err := markSeen(seen, key.Value); err != nil {
			return fmt.Errorf("reviews: %w", err)
		}
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

// decodePaths parses a tier's `paths:` block into ordered PathRules, rejecting
// invalid directory keys and post-normalization duplicates (e.g. "/config" and
// "config/" collapsing to the same directory).
func decodePaths(node *yaml.Node) ([]domain.PathRule, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("paths: expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return nil, fmt.Errorf("paths: malformed mapping")
	}
	out := make([]domain.PathRule, 0, len(node.Content)/2)
	seenDir := map[string]string{} // normalized dir -> original key, for collision reporting
	for i := 0; i < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("paths: non-scalar key")
		}
		dir, err := normalizePathKey(keyNode.Value)
		if err != nil {
			return nil, err
		}
		if prev, ok := seenDir[dir]; ok {
			return nil, fmt.Errorf("paths: keys %q and %q refer to the same directory %q", prev, keyNode.Value, dir)
		}
		seenDir[dir] = keyNode.Value
		rule := domain.PathRule{Dir: dir}
		if err := decodePathRule(&rule, valNode); err != nil {
			return nil, fmt.Errorf("paths %q: %w", keyNode.Value, err)
		}
		out = append(out, rule)
	}
	return out, nil
}

// normalizePathKey canonicalizes a path key to a repo-relative directory with
// no leading/trailing slash. It rejects empty/root keys and any key with a ".."
// segment (which would escape the repository). GitHub returns repo-relative
// file paths, so "/config", "config", and "config/" all normalize to "config".
func normalizePathKey(raw string) (string, error) {
	s := strings.Trim(strings.TrimSpace(raw), "/")
	if s == "" {
		return "", fmt.Errorf("paths: key %q is empty or root; give a directory like \"services/payments\"", raw)
	}
	if slices.Contains(strings.Split(s, "/"), "..") {
		return "", fmt.Errorf("paths: key %q must not contain \"..\"", raw)
	}
	c := path.Clean(s)
	if c == "." || c == "/" {
		return "", fmt.Errorf("paths: key %q normalizes to root", raw)
	}
	return c, nil
}

// decodePathRule parses one path node (channel + tri-state mentions), rejecting
// unknown and duplicate keys.
func decodePathRule(rule *domain.PathRule, node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping; got node kind %d", node.Kind)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("malformed mapping")
	}
	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("non-scalar key")
		}
		if err := markSeen(seen, keyNode.Value); err != nil {
			return err
		}
		switch keyNode.Value {
		case "channel":
			if err := valNode.Decode(&rule.Channel); err != nil {
				return fmt.Errorf("channel: %w", err)
			}
		case "mentions":
			rule.MentionsPresent = true
			if isNullNode(valNode) {
				return fmt.Errorf("mentions: null is not allowed; omit the key to inherit or use [] for none")
			}
			if valNode.Kind != yaml.SequenceNode {
				return fmt.Errorf("mentions: must be a list (use [] for none, omit the key to inherit)")
			}
			ms := []string{}
			if err := valNode.Decode(&ms); err != nil {
				return fmt.Errorf("mentions: %w", err)
			}
			rule.Mentions = ms
		default:
			return fmt.Errorf("unknown field %q", keyNode.Value)
		}
	}
	return nil
}

// markSeen records key in seen, returning an error if it was already present.
// The hand-rolled decoders walk the raw node, so yaml.v3's duplicate-key
// detection (which only fires when decoding into a Go map/struct) does not
// apply; without this guard a repeated key would silently take the last value.
func markSeen(seen map[string]bool, key string) error {
	if seen[key] {
		return fmt.Errorf("duplicate key %q", key)
	}
	seen[key] = true
	return nil
}

func isNullNode(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || n.Value == "null" || n.Value == "~")
}

func (d digestConfigWire) toDomain() domain.DigestConfig {
	return domain.DigestConfig{Enabled: d.Enabled, Schedule: d.Schedule, Timezone: d.Timezone}
}

func (r reactionsOverrideWire) toDomain() domain.ReactionsOverride {
	return domain.ReactionsOverride{
		Enabled:       r.Enabled,
		NewPR:         r.NewPR,
		MergedPR:      r.MergedPR,
		ClosedPR:      r.ClosedPR,
		Approved:      r.Approved,
		Commented:     r.Commented,
		RequestChange: r.RequestChange,
		BotReview:     r.BotReview,
	}
}

func (rc repoConfigWire) toDomain() domain.RepoConfig {
	out := domain.RepoConfig{
		Channel:          rc.Channel,
		Mentions:         rc.Mentions,
		MentionsPresent:  rc.MentionsPresent,
		IgnoreAIReviews:  rc.IgnoreAIReviews,
		DependabotFormat: rc.DependabotFormat,
		Paths:            rc.Paths,
	}
	if rc.Reactions != nil {
		v := rc.Reactions.toDomain()
		out.Reactions = &v
	}
	if rc.Digest != nil {
		v := rc.Digest.toDomain()
		out.Digest = &v
	}
	return out
}

// DecodeMappings decodes a raw `mappings:` YAML node through the wire codec,
// preserving the tri-state mentions semantics, per-tier behavioral blocks
// (reactions/reviews/digest/paths), unknown-key rejection, and duplicate-key
// detection. platform/config routes config.yaml's mappings section here so
// the file and the domain types stay decoupled. A null node (bare key) decodes
// as absent.
func DecodeMappings(node *yaml.Node) (map[string]domain.Org, error) {
	if node.Tag == "!!null" {
		return nil, nil
	}
	var wire map[string]map[string]repoConfigWire
	if err := node.Decode(&wire); err != nil {
		return nil, fmt.Errorf("mappings: %w", err)
	}
	out := make(map[string]domain.Org, len(wire))
	for org, repos := range wire {
		tiers := make(domain.Org, len(repos))
		for name, repoConfig := range repos {
			tiers[name] = repoConfig.toDomain()
		}
		out[org] = tiers
	}
	return out, nil
}

// DecodeDigest decodes a raw global `digest:` YAML node through the wire
// codec, defaulting enabled to true when the key is absent. A null node (bare
// key) decodes as absent.
func DecodeDigest(node *yaml.Node) (*domain.DigestConfig, error) {
	if node.Tag == "!!null" {
		return nil, nil
	}
	var wire digestConfigWire
	if err := node.Decode(&wire); err != nil {
		return nil, fmt.Errorf("digest: %w", err)
	}
	out := wire.toDomain()
	return &out, nil
}
