package domain

import "github.com/mptooling/notifycat/internal/kernel"

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
	AI               *AIOverride

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

// Reactions is the resolved per-repo reaction-emoji set (Slack emoji names
// without colons). Enabled gates whether close/review reactions are added at
// all. Empty BotReview disables the bot-reviewer marker.
type Reactions struct {
	Enabled       bool
	NewPR         string
	MergedPR      string
	ClosedPR      string
	Approved      string
	Commented     string
	RequestChange string
	BotReview     string
}

// AIOverride is a tier's optional `ai:` block. Enabled is tri-state (nil =
// inherit); Instructions concatenates onto the less-specific tiers' guidance
// rather than replacing it. Provider, model, and key are deliberately not
// per-tier — one provider per deployment, mirroring git_provider.
type AIOverride struct {
	Enabled      *bool
	Instructions string
}

// Target is one fan-out destination resolved for a PR: a channel and the
// mentions to ping there. Produced by the mappings resolver, consumed by the
// open handler.
type Target struct {
	Channel  string
	Mentions []string
}

// RepoMapping is the value object handlers and validators consume — a GitHub
// repository routed to a Slack channel with an optional mentions list, and
// resolved behavioral config (global defaults merged with org/* and org/repo
// overrides). The source of truth for routing lives in config.yaml's mappings:
// section (loaded by internal/config / internal/mappings); the type stays here
// so consumers don't have to know who produced it.
type RepoMapping struct {
	Repository   string
	SlackChannel string
	Mentions     []string
	// Resolved per-repo behavioral config (global config.yaml defaults merged
	// with org/* and org/repo overrides). Formatting-only — not part of
	// validation or the lock.
	Reactions        Reactions
	IgnoreAIReviews  bool
	DependabotFormat bool
	// AIEnabled and AIInstructions are the resolved per-tier ai settings:
	// enabled tri-state merged across tiers, instructions concatenated
	// global → org/* → org/repo. Not part of validation or the lock.
	AIEnabled      bool
	AIInstructions string
}

// Resolved is the effective routing config for one repository after merging
// the org/repo tier over the org/* tier.
type Resolved struct {
	Channel  string
	Mentions []string
}

// Defaults is the global tier: the config.yaml top-level behavioral settings
// that per-repo tiers override.
type Defaults struct {
	Reactions        Reactions
	IgnoreAIReviews  bool
	DependabotFormat bool
	// AIEnabled/AIInstructions mirror the global ai: config block (filled by the composition root).
	AIEnabled      bool
	AIInstructions string
	// GitProvider is the deployment's single git_provider; the Provider stamps it
	// on every entry so it hashes into the lock (see Entry.Provider).
	GitProvider kernel.Provider
}

// ResolvedTargets is the full fan-out resolution for one PR: the repo's
// behavioral mapping, the per-channel targets, and the changed files the
// router already fetched for path routing — kept on the result so the
// salience advisor can reuse them without a second provider call. ChangedFiles
// is nil when no fetcher is configured, the repo has no path rules, or the
// fetch soft-failed.
type ResolvedTargets struct {
	Mapping      RepoMapping
	Targets      []Target
	ChangedFiles []string
}
