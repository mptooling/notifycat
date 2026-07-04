package application

import (
	"regexp"
	"strings"

	"github.com/mptooling/notifycat/internal/notification/domain"
)

const (
	dependabotLogin = "dependabot[bot]"
	renovateLogin   = "renovate[bot]"
)

// DetectBot matches login against the two known dependency-bot logins,
// case-insensitively. The surface is exactly two values, so the match is exact —
// prefix matching ("dependabot") is intentionally not a hit. Callers pass the PR
// author so the classification follows who opened the PR, not who fired the
// webhook.
func DetectBot(login string) domain.BotKind {
	switch strings.ToLower(login) {
	case dependabotLogin:
		return domain.BotKindDependabot
	case renovateLogin:
		return domain.BotKindRenovate
	default:
		return domain.BotKindNone
	}
}

// vulnerabilityMarker matches the Markdown header a dependency bot inserts when a
// PR fixes a security advisory ("## Vulnerabilities fixed" / "### Vulnerabilities").
// Anchored to header lines so a bare mention of "vulnerability" in prose or a
// changelog line does not trip it.
var vulnerabilityMarker = regexp.MustCompile(`(?im)^#+\s*.*vulnerabilit(y|ies)`)

// IsSecurityAdvisory reports whether prBody carries the advisory section that
// marks a security update. A miss returns false (routine) — the safe default.
func IsSecurityAdvisory(prBody string) bool {
	return vulnerabilityMarker.MatchString(prBody)
}
