// Package aireview identifies webhook events from a "bot reviewer" (any
// account whose sender.type is "Bot") and exposes two orthogonal policies
// over that identity. The detector is a tiny value object so callers can
// short-circuit cheaply.
//
//   - ShouldSuppress is the opt-in mute switch (NOTIFYCAT_IGNORE_AI_REVIEWS):
//     when enabled, a bot reviewer's reaction is skipped entirely.
//   - IsBot is the bare identity check, independent of that flag, used to add
//     a distinct bot-review marker reaction when the bot is *not* suppressed.
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

// IsBot reports whether the given sender.type is a bot, regardless of the
// suppression flag. Callers use it to add the distinct bot-review marker once
// they have already passed the ShouldSuppress gate. Matches "Bot" only.
func (d *Detector) IsBot(senderType string) bool {
	return senderType == senderTypeBot
}
