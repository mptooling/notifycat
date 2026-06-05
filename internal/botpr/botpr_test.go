package botpr_test

import (
	"testing"

	"github.com/mptooling/notifycat/internal/botpr"
)

func TestDetectBot(t *testing.T) {
	cases := []struct {
		name  string
		login string
		want  botpr.BotKind
	}{
		{"dependabot", "dependabot[bot]", botpr.BotKindDependabot},
		{"renovate", "renovate[bot]", botpr.BotKindRenovate},
		{"dependabot mixed case", "Dependabot[Bot]", botpr.BotKindDependabot},
		{"renovate upper", "RENOVATE[BOT]", botpr.BotKindRenovate},
		{"human", "alice", botpr.BotKindNone},
		{"other bot", "github-actions[bot]", botpr.BotKindNone},
		{"empty", "", botpr.BotKindNone},
		{"prefix is not a match", "dependabot", botpr.BotKindNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := botpr.DetectBot(c.login); got != c.want {
				t.Errorf("DetectBot(%q) = %v; want %v", c.login, got, c.want)
			}
		})
	}
}

func TestBotKind_Name(t *testing.T) {
	cases := []struct {
		kind botpr.BotKind
		want string
	}{
		{botpr.BotKindDependabot, "dependabot"},
		{botpr.BotKindRenovate, "renovate"},
		{botpr.BotKindNone, ""},
	}
	for _, c := range cases {
		if got := c.kind.Name(); got != c.want {
			t.Errorf("BotKind(%d).Name() = %q; want %q", c.kind, got, c.want)
		}
	}
}

func TestIsSecurityAdvisory(t *testing.T) {
	// Mirrors the structured header Dependabot inserts for advisory PRs.
	dependabotSecurity := `Bumps acme/lib from 1.2.0 to 1.2.1.

## Vulnerabilities fixed

Sourced from the GitHub Security Advisory Database.

> CVE-2026-1234: a thing
`
	// Renovate's section header when vulnerabilityAlerts is enabled.
	renovateSecurity := `This PR contains the following updates.

### Vulnerabilities

This update fixes a known vulnerability.
`
	routine := `Bumps acme/lib from 1.2.0 to 1.2.1.

## Release notes

- Fixed a typo.
`
	// "vulnerability" only in prose / a release-note line, not a header.
	proseOnly := `Bumps acme/lib from 1.2.0 to 1.2.1.

## Release notes

- This release mentions a vulnerability in the changelog but is a routine bump.
`

	cases := []struct {
		name string
		body string
		want bool
	}{
		{"dependabot vulnerabilities header", dependabotSecurity, true},
		{"renovate vulnerabilities header", renovateSecurity, true},
		{"routine bump", routine, false},
		{"empty body", "", false},
		{"prose-only mention is not a match", proseOnly, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := botpr.IsSecurityAdvisory(c.body); got != c.want {
				t.Errorf("IsSecurityAdvisory(...) = %v; want %v\nbody:\n%s", got, c.want, c.body)
			}
		})
	}
}
