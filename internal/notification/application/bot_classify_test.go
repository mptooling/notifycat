package application_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/notification/application"
	"github.com/mptooling/notifycat/internal/notification/domain"
)

func TestDetectBot(t *testing.T) {
	testCases := []struct {
		name  string
		login string
		want  domain.BotKind
	}{
		{"dependabot", "dependabot[bot]", domain.BotKindDependabot},
		{"renovate", "renovate[bot]", domain.BotKindRenovate},
		{"dependabot mixed case", "Dependabot[Bot]", domain.BotKindDependabot},
		{"renovate upper", "RENOVATE[BOT]", domain.BotKindRenovate},
		{"human", "alice", domain.BotKindNone},
		{"other bot", "github-actions[bot]", domain.BotKindNone},
		{"empty", "", domain.BotKindNone},
		{"prefix is not a match", "dependabot", domain.BotKindNone},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, application.DetectBot(testCase.login))
		})
	}
}

func TestBotKind_Name(t *testing.T) {
	testCases := []struct {
		kind domain.BotKind
		want string
	}{
		{domain.BotKindDependabot, "dependabot"},
		{domain.BotKindRenovate, "renovate"},
		{domain.BotKindNone, ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.want, func(t *testing.T) {
			assert.Equal(t, testCase.want, testCase.kind.Name())
		})
	}
}

func TestIsSecurityAdvisory(t *testing.T) {
	// Mirrors the structured header Dependabot inserts for advisory PRs.
	const dependabotSecurity = `Bumps acme/lib from 1.2.0 to 1.2.1.

## Vulnerabilities fixed

Sourced from the GitHub Security Advisory Database.

> CVE-2026-1234: a thing
`
	// Renovate's section header when vulnerabilityAlerts is enabled.
	const renovateSecurity = `This PR contains the following updates.

### Vulnerabilities

This update fixes a known vulnerability.
`
	const routine = `Bumps acme/lib from 1.2.0 to 1.2.1.

## Release notes

- Fixed a typo.
`
	// "vulnerability" only in prose / a release-note line, not a header.
	const proseOnly = `Bumps acme/lib from 1.2.0 to 1.2.1.

## Release notes

- This release mentions a vulnerability in the changelog but is a routine bump.
`

	testCases := []struct {
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

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, application.IsSecurityAdvisory(testCase.body))
		})
	}
}
