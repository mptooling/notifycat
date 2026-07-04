package domain

import "github.com/mptooling/notifycat/internal/kernel"

// IsBot reports whether a webhook sender is a bot (a GitHub App). The reaction
// handlers use it to suppress reactions to automated reviews when a repo enables
// IgnoreAIReviews. It intentionally does not distinguish AI reviewers from
// scripted bots — Copilot, dependabot, and github-actions all present as "Bot"
// in the payload.
func IsBot(senderType string) bool {
	return senderType == kernel.SenderTypeBot
}
