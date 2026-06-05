// Package botpr classifies pull requests opened by dependency-update bots
// (Dependabot, Renovate) so the notifier can render a compact Slack message
// instead of the standard "please review" format, and tell routine version
// bumps apart from security-advisory updates.
//
// Detection is deliberately narrow: an exact, case-insensitive match on the
// two known bot logins (no prefix matching, no operator-configurable
// allowlist), and a conservative header-anchored scan of the PR body for the
// advisory section. A body parse miss falls back to the routine kind — never
// the other way around — so a template change on GitHub's side degrades a
// security PR to routine, not a routine PR to a false alarm.
package botpr

import (
	"regexp"
	"strings"
)

// BotKind identifies which dependency bot opened a PR, or BotKindNone for
// anything else (humans and unrecognised bots alike).
type BotKind int

// Bot kinds returned by DetectBot. BotKindNone covers humans and any bot that
// is not one of the two recognised dependency updaters.
const (
	BotKindNone BotKind = iota
	BotKindDependabot
	BotKindRenovate
)

const (
	dependabotLogin = "dependabot[bot]"
	renovateLogin   = "renovate[bot]"
)

// Name returns the lowercase bot name used in the composed message
// ("dependabot" / "renovate"), or "" for BotKindNone.
func (k BotKind) Name() string {
	switch k {
	case BotKindDependabot:
		return "dependabot"
	case BotKindRenovate:
		return "renovate"
	default:
		return ""
	}
}

// DetectBot matches login against the two known bot logins, case-insensitively.
// The surface is exactly two values, so the match is exact — prefix matching
// ("dependabot") is intentionally not a hit. Callers pass the PR author so the
// classification follows who opened the PR, not who fired the webhook.
func DetectBot(login string) BotKind {
	switch strings.ToLower(login) {
	case dependabotLogin:
		return BotKindDependabot
	case renovateLogin:
		return BotKindRenovate
	default:
		return BotKindNone
	}
}

// vulnerabilityMarker matches the Markdown header Dependabot inserts when a PR
// fixes a security advisory ("## Vulnerabilities fixed") and Renovate's
// equivalent "### Vulnerabilities" section. Anchored to header lines so a bare
// mention of "vulnerability" in prose or a changelog line does not trip it.
var vulnerabilityMarker = regexp.MustCompile(`(?im)^#+\s*.*vulnerabilit(y|ies)`)

// IsSecurityAdvisory reports whether prBody carries the advisory section that
// marks a security update. A miss returns false (routine) — the safe default.
func IsSecurityAdvisory(prBody string) bool {
	return vulnerabilityMarker.MatchString(prBody)
}
