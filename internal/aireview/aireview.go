// Package aireview decides whether a webhook event from a "bot reviewer"
// (any account whose sender.type is "Bot") should have its Slack reaction
// suppressed. The detector is a tiny value object so callers can short-
// circuit cheaply when the feature flag is off.
//
// The opt-in flag is global and defaults off; see internal/config.
// Detection is deliberately broad: GitHub's payload does not distinguish AI
// reviewers (Copilot, Claude, …) from scripted bots (dependabot, renovate,
// release-please, …), so operators who enable the flag accept that any
// non-human reviewer is silenced. See docs/operations.md for the trade-off.
package aireview

// senderTypeBot is the exact string GitHub uses on `sender.type` for any
// non-user account (GitHub Apps and legacy bot users). Matching is
// case-sensitive on purpose — anything else must not be treated as a bot.
const senderTypeBot = "Bot"

// Detector decides whether to suppress reactions for an event whose sender
// is a bot. Zero value is a disabled detector that never suppresses.
type Detector struct {
	enabled bool
}

// NewDetector returns a Detector. When enabled is false, ShouldSuppress
// always returns false without inspecting its argument.
func NewDetector(enabled bool) *Detector { return &Detector{enabled: enabled} }

// ShouldSuppress reports whether a reaction for an event with the given
// sender.type should be skipped. Matches GitHub's literal "Bot" only.
func (d *Detector) ShouldSuppress(senderType string) bool {
	return d.enabled && senderType == senderTypeBot
}
