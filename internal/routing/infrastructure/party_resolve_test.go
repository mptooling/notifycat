package infrastructure_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/routing/application"
	domain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/routing/infrastructure"
)

const partyLevelsDocument = `
mappings:
  acme:
    "*":
      channel: C0STAR00000
      party_reviewers: 3
    api:
      party_deadline: 2h
      paths:
        billing: {party_reviewers: 4, party_deadline: 15m}
    web:
      channel: C0WEB000000
      party_reviewers: 5
  beta:
    api:
      channel: C0BETA00000
`

const partyPathsDocument = `
mappings:
  acme:
    mono:
      channel: C0MONO00000
      party_reviewers: 2
      party_deadline: 2h
      paths:
        docs: {party_reviewers: 1, party_deadline: 4h}
        services: {mentions: ["<@U0S>"], party_reviewers: 4, party_deadline: 30m}
        services/billing: {mentions: ["<@U0B>"]}
        services/search: {party_deadline: 3h}
        tools: {party_reviewers: 3, party_deadline: 1h}
`

func partyProviderFromDoc(t *testing.T, body string, global domain.PartySettings) *application.Provider {
	t.Helper()

	file, err := infrastructure.Parse(strings.NewReader(body))
	require.NoError(t, err)
	return application.NewProvider(domain.Defaults{Party: global}, file.Mappings, nil)
}

func TestProvider_PartySettingsLevels(t *testing.T) {
	provider := partyProviderFromDoc(t, partyLevelsDocument, domain.PartySettings{Reviewers: 2, Deadline: 90 * time.Minute})
	testCases := []struct {
		name       string
		repository string
		files      []string
		want       domain.PartySettings
	}{
		{"global", "beta/api", nil, domain.PartySettings{Reviewers: 2, Deadline: 90 * time.Minute}},
		{"org star", "acme/other", nil, domain.PartySettings{Reviewers: 3, Deadline: 90 * time.Minute}},
		{"repository tier", "acme/web", nil, domain.PartySettings{Reviewers: 5, Deadline: 90 * time.Minute}},
		{"star cap, tier timer", "acme/api", nil, domain.PartySettings{Reviewers: 3, Deadline: 2 * time.Hour}},
		{"path", "acme/api", []string{"/billing/x.go"}, domain.PartySettings{Reviewers: 4, Deadline: 15 * time.Minute}},
		{"file outside the paths", "acme/api", []string{"README.md"}, domain.PartySettings{Reviewers: 3, Deadline: 2 * time.Hour}},
		{"unmapped", "gamma/api", nil, domain.PartySettings{Reviewers: 2, Deadline: 90 * time.Minute}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := provider.PartySettings(testCase.repository, testCase.files)

			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestProvider_PartySettingsBuiltInDefaults(t *testing.T) {
	provider := partyProviderFromDoc(t, partyLevelsDocument, domain.PartySettings{})

	got := provider.PartySettings("beta/api", nil)

	assert.Equal(t, domain.PartySettings{Reviewers: domain.DefaultPartyReviewers, Deadline: domain.DefaultPartyDeadline}, got)
}

func TestProvider_PartySettingsGlobalBelowMinimumFallsBackPerKey(t *testing.T) {
	provider := partyProviderFromDoc(t, partyLevelsDocument, domain.PartySettings{Reviewers: 3, Deadline: time.Minute})

	got := provider.PartySettings("beta/api", nil)

	assert.Equal(t, domain.PartySettings{Reviewers: 3, Deadline: domain.DefaultPartyDeadline}, got,
		"a global timer below the minimum must not lower the timer")
}

func TestProvider_PartySettingsAcrossPaths(t *testing.T) {
	provider := partyProviderFromDoc(t, partyPathsDocument, domain.PartySettings{})
	testCases := []struct {
		name  string
		files []string
		want  domain.PartySettings
	}{
		{"spec example 1", []string{"docs/a.md", "README.md"}, domain.PartySettings{Reviewers: 2, Deadline: 2 * time.Hour}},
		{"spec example 2", []string{"services/billing/x.go"}, domain.PartySettings{Reviewers: 4, Deadline: 30 * time.Minute}},
		{"each key from its most specific rule", []string{"services/search/y.go"}, domain.PartySettings{Reviewers: 4, Deadline: 3 * time.Hour}},
		{"highest cap, shortest timer", []string{"tools/a.sh", "services/billing/x.go"}, domain.PartySettings{Reviewers: 4, Deadline: 30 * time.Minute}},
		{"a file outside the paths counts", []string{"tools/a.sh", "README.md"}, domain.PartySettings{Reviewers: 3, Deadline: time.Hour}},
		{"segment match only", []string{"toolsx/a.sh"}, domain.PartySettings{Reviewers: 2, Deadline: 2 * time.Hour}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := provider.PartySettings("acme/mono", testCase.files)

			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestProvider_PartySettingsFoldsCase(t *testing.T) {
	provider := partyProviderFromDoc(t, `
mappings:
  Acme:
    API:
      channel: C0API000000
      party_reviewers: 3
      paths:
        billing: {party_reviewers: 4}
`, domain.PartySettings{})

	assert.Equal(t, 3, provider.PartySettings("acme/api", nil).Reviewers)
	assert.Equal(t, 4, provider.PartySettings("acme/api", []string{"billing/x.go"}).Reviewers)
	assert.True(t, provider.HasPartyPathRules("acme/api"))
}

func TestProvider_HasPartyPathRules(t *testing.T) {
	testCases := []struct {
		name       string
		document   string
		repository string
		want       bool
	}{
		{"party path rule", partyLevelsDocument, "acme/api", true},
		{"mixed rules", partyPathsDocument, "acme/mono", true},
		{"tier without paths", partyLevelsDocument, "acme/web", false},
		{"slack paths only", monorepoDoc, "acme/the-monorepo", false},
		{"unmapped", partyLevelsDocument, "gamma/api", false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			provider := partyProviderFromDoc(t, testCase.document, domain.PartySettings{})

			got := provider.HasPartyPathRules(testCase.repository)

			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestProvider_PartySettingsPartyOnlyEntriesStayInLookup(t *testing.T) {
	provider := partyProviderFromDoc(t, `
mappings:
  acme:
    "*":
      channel: C0STAR00000
      party_reviewers: 3
    mono:
      paths:
        tools: {party_deadline: 30m}
`, domain.PartySettings{})

	assert.Equal(t, 3, provider.PartySettings("acme/other", nil).Reviewers, "a party-only star tier still sets the cap")
	assert.Equal(t, 30*time.Minute, provider.PartySettings("acme/mono", []string{"tools/a.sh"}).Deadline)
	assert.True(t, provider.HasPartyPathRules("acme/mono"))
}

func TestProvider_PartySettingsNestedRuleOverridesOuterRule(t *testing.T) {
	provider := partyProviderFromDoc(t, `
mappings:
  acme:
    mono:
      channel: C0MONO00000
      paths:
        services: {party_reviewers: 4, party_deadline: 3h}
        services/billing: {party_reviewers: 2, party_deadline: 20m}
`, domain.PartySettings{})

	got := provider.PartySettings("acme/mono", []string{"services/billing/x.go"})

	assert.Equal(t, domain.PartySettings{Reviewers: 2, Deadline: 20 * time.Minute}, got,
		"the most specific rule wins even when it lowers the values")
}
