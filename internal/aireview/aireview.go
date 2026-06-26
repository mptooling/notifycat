// Package aireview identifies webhook events from a "bot reviewer" (any
// account whose sender.type is "Bot"). Detection is deliberately broad:
// GitHub's payload does not distinguish AI reviewers (Copilot, Claude, …)
// from scripted bots (dependabot, renovate, release-please, …), so callers
// see the identity but suppression policy is applied per-repo by the
// event handlers.
package aireview

// senderTypeBot is the exact string GitHub uses on `sender.type` for any
// non-user account (GitHub Apps and legacy bot users). Matching is
// case-sensitive on purpose — anything else must not be treated as a bot.
const senderTypeBot = "Bot"

// Detector is a pure bot classifier. It has no state and serves as a
// namespace for the bot-detection logic.
type Detector struct{}

// NewDetector returns a Detector.
func NewDetector() *Detector { return &Detector{} }

// IsBot reports whether the given sender.type is a bot. Matches "Bot" only.
func (d *Detector) IsBot(senderType string) bool {
	return senderType == senderTypeBot
}
