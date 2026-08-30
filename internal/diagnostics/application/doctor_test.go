package application_test

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/diagnostics/application"
	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// fakeRepoValidator satisfies validationdomain.RepoValidator without real
// Slack/GitHub clients.
type fakeRepoValidator struct {
	got    string
	report validationdomain.Report
}

func (f *fakeRepoValidator) Validate(_ context.Context, repository string) validationdomain.Report {
	f.got = repository
	if f.report.Repository == "" {
		f.report.Repository = repository
	}
	return f.report
}

// validSnapshot returns a ConfigSnapshot that passes all checks. The snapshot
// exposes only boolean flags for secrets — no raw secret values.
func validSnapshot() diagnosticsdomain.ConfigSnapshot {
	return diagnosticsdomain.ConfigSnapshot{
		ConfigFile:       "./config.yaml",
		DatabaseURL:      "file:./data/notifycat.db",
		Domain:           "",
		MessageTTLDays:   30,
		WebhookSecretSet: true,
		WebhookSecretVar: "GITHUB_WEBHOOK_SECRET",
		SlackTokenSet:    true,
		TokenSet:         false,
		TokenVar:         "GITHUB_TOKEN",
		DatabaseOpenable: true,
		DatabaseDetail:   "file:./data/notifycat.db",
	}
}

func oneEntrySnapshot() diagnosticsdomain.ConfigSnapshot {
	snapshot := validSnapshot()
	snapshot.Entries = []routingdomain.Entry{
		{Org: "octo", Repo: "widget", Channel: "C0123ABCDE"},
	}
	return snapshot
}

// findSectionCheck returns the named check, or fails the test.
func findSectionCheck(t *testing.T, section diagnosticsdomain.Section, name string) validationdomain.CheckResult {
	t.Helper()

	for _, check := range section.Checks {
		if check.Name == name {
			return check
		}
	}
	require.FailNowf(t, "missing check", "no %q check in section: %+v", name, section.Checks)
	return validationdomain.CheckResult{}
}

// failedCheckNames lists every check in the section that failed.
func failedCheckNames(section diagnosticsdomain.Section) []string {
	var failed []string
	for _, check := range section.Checks {
		if check.Status == validationdomain.StatusFail {
			failed = append(failed, check.Name)
		}
	}
	return failed
}

func TestCheckConfig_AllSetReturnsOK(t *testing.T) {
	section := application.CheckConfig(validSnapshot())

	assert.Equal(t, "config", section.Name)
	assert.True(t, section.OK(), "checks: %+v", section.Checks)
}

func TestCheckConfig_MissingSecretsFail(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.WebhookSecretSet = false
	snapshot.SlackTokenSet = false

	section := application.CheckConfig(snapshot)

	assert.False(t, section.OK())
	assert.Subset(t, failedCheckNames(section), []string{"GITHUB_WEBHOOK_SECRET", "SLACK_BOT_TOKEN"})
}

// The snapshot exposes only booleans for secrets — the detail fields never
// contain a raw value.
func TestCheckConfig_NeverPrintsSecretValues(t *testing.T) {
	nonSecretChecks := []string{"cleanup.message_ttl_days", "database.url", "config.yaml", "server.domain"}

	section := application.CheckConfig(validSnapshot())

	for _, check := range section.Checks {
		if slices.Contains(nonSecretChecks, check.Name) {
			continue
		}
		assert.Contains(t, []string{"set", "missing; set the environment variable"}, check.Detail,
			"secret check %q must report only set/missing", check.Name)
	}
}

func TestCheckConfig_ValidDomainReportsWebhookURL(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.Domain = "notifycat.example.com"

	section := application.CheckConfig(snapshot)

	check := findSectionCheck(t, section, "server.domain")
	assert.Equal(t, validationdomain.StatusOK, check.Status)
	assert.Contains(t, check.Detail, "https://notifycat.example.com/webhook/github",
		"the detail names the exact URL to paste into GitHub")
}

func TestCheckConfig_DomainWithSchemeFails(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.Domain = "https://notifycat.example.com"

	section := application.CheckConfig(snapshot)

	assert.Equal(t, validationdomain.StatusFail, findSectionCheck(t, section, "server.domain").Status,
		"the domain must be a bare host")
	assert.False(t, section.OK())
}

func TestCheckConfig_MalformedDomainFails(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.Domain = "not a valid host"

	section := application.CheckConfig(snapshot)

	assert.Equal(t, validationdomain.StatusFail, findSectionCheck(t, section, "server.domain").Status)
}

func TestCheckConfig_UnsetDomainSkips(t *testing.T) {
	section := application.CheckConfig(validSnapshot())

	assert.Equal(t, validationdomain.StatusSkip, findSectionCheck(t, section, "server.domain").Status,
		"local-dev and tunnel users run without a domain")
	assert.True(t, section.OK())
}

func TestCheckConfig_RejectsNonPositiveTTL(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.MessageTTLDays = 0

	section := application.CheckConfig(snapshot)

	assert.False(t, section.OK())
}

func TestCheckDatabase_OpenableReturnsOK(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.DatabaseURL = "file:/some/path/doctor.db"
	snapshot.DatabaseOpenable = true
	snapshot.DatabaseDetail = snapshot.DatabaseURL

	section := application.CheckDatabase(snapshot)

	assert.Equal(t, "database", section.Name)
	assert.True(t, section.OK(), "checks: %+v", section.Checks)
}

func TestCheckDatabase_NotOpenableFails(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.DatabaseURL = "file:/this/path/does/not/exist/doctor.db"
	snapshot.DatabaseOpenable = false
	snapshot.DatabaseDetail = `cannot open "file:/this/path/does/not/exist/doctor.db": store: open: ...; ensure the parent directory exists and is writable`

	section := application.CheckDatabase(snapshot)

	assert.False(t, section.OK())
}

func TestCheckDatabase_EmptyDSNFails(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.DatabaseURL = ""
	snapshot.DatabaseOpenable = false
	snapshot.DatabaseDetail = ""

	section := application.CheckDatabase(snapshot)

	assert.False(t, section.OK())
}

func TestCheckMappings_WithEntriesIsOK(t *testing.T) {
	section := application.CheckMappings(oneEntrySnapshot())

	assert.Equal(t, "mappings", section.Name)
	assert.True(t, section.OK(), "checks: %+v", section.Checks)
	require.NotEmpty(t, section.Checks)
	assert.Contains(t, section.Checks[0].Detail, "1 entries")
}

func TestCheckMappings_EmptyMappingsIsOK(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.Entries = nil

	section := application.CheckMappings(snapshot)

	assert.True(t, section.OK(), "an operator may boot with no mappings yet")
}

func TestCheckMappings_PathRoutingActiveWithToken(t *testing.T) {
	snapshot := oneEntrySnapshot()
	snapshot.HasPathRules = true
	snapshot.TokenSet = true

	section := application.CheckMappings(snapshot)

	assert.Equal(t, validationdomain.StatusOK, findSectionCheck(t, section, "path routing").Status)
	assert.True(t, section.OK())
}

func TestCheckMappings_PathRoutingInertWithoutToken(t *testing.T) {
	snapshot := oneEntrySnapshot()
	snapshot.HasPathRules = true
	snapshot.TokenSet = false

	section := application.CheckMappings(snapshot)

	assert.Equal(t, validationdomain.StatusSkip, findSectionCheck(t, section, "path routing").Status,
		"path routing needs a read token to do anything")
	assert.True(t, section.OK(), "inert path routing is a SKIP, not a failure")
}

func TestDoctorRun_AlwaysReturnsConfigDatabaseMappings(t *testing.T) {
	doctor := application.NewDoctor(validSnapshot(), nil)

	sections := doctor.Run(context.Background(), "")

	require.Len(t, sections, 3)
	assert.Equal(t, []string{"config", "database", "mappings"},
		[]string{sections[0].Name, sections[1].Name, sections[2].Name})
}

func TestDoctorRun_TargetRepositoryDelegatesToValidator(t *testing.T) {
	validator := &fakeRepoValidator{
		report: validationdomain.Report{
			Repository: "octo/widget",
			Checks: []validationdomain.CheckResult{
				{Name: "slack-auth", Status: validationdomain.StatusOK, Detail: "ok"},
				{Name: "slack-channel", Status: validationdomain.StatusFail, Detail: "bot not in channel"},
			},
		},
	}
	doctor := application.NewDoctor(validSnapshot(), validator)

	sections := doctor.Run(context.Background(), "octo/widget")

	assert.Equal(t, "octo/widget", validator.got)
	require.Len(t, sections, 4, "the repo section is appended after config/database/mappings")
	assert.Equal(t, "octo/widget", sections[3].Name)
	assert.False(t, sections[3].OK(), "the section mirrors the validator's verdict")
}

func TestDoctorRun_TargetRepositoryWithoutValidatorIsNoop(t *testing.T) {
	doctor := application.NewDoctor(validSnapshot(), nil)

	sections := doctor.Run(context.Background(), "octo/widget")

	assert.Len(t, sections, 3, "with no validator wired the repo target is ignored")
}
