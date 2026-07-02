package mappings

// ChannelMention is the Slack wire token used when an entry omits the
// `mentions:` key — operators see `@channel` in Slack.
const ChannelMention = "<!channel>"

// File is the parsed mappings.yaml document.
type File struct {
	Digest   *DigestConfig  `yaml:"digest"`
	Mappings map[string]Org `yaml:"mappings"`
}

// DigestConfig is the `digest:` section: a scheduled reminder that lists open
// PRs nobody has touched since the previous day. The global section is optional
// and the feature is on by default, so an absent section behaves like
// `{enabled: true}` with the default schedule. Per-repo tiers reuse this type
// to override Enabled/Schedule.
//
// Timezone is global-only — the server runs a single cron clock, so the zone
// belongs on the global section and is rejected on a per-repo tier. Empty means
// the default (UTC); config.Load resolves it to a *time.Location and fails fast
// on an invalid zone.
type DigestConfig struct {
	Enabled  bool
	Schedule string
	Timezone string
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

	// Paths is the optional per-directory routing for a monorepo, in
	// declaration order (order is significant for tie-breaking). Only valid on
	// a named repo tier — ValidateMappings rejects it on the "*" tier.
	Paths []PathRule
}

// PathRule is one entry in a repo tier's `paths:` block: a normalized
// directory and the routing applied to files under it. Channel/Mentions carry
// the same tri-state inheritance as a repo tier (empty Channel inherits;
// MentionsPresent distinguishes absent from an explicit empty list).
type PathRule struct {
	Dir             string
	Channel         string
	Mentions        []string
	MentionsPresent bool
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
