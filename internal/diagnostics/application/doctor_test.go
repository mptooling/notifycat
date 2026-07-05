package application_test

import (
	"context"
	"strings"
	"testing"

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

// ---- CheckConfig tests ----

func TestCheckConfig_AllSetReturnsOK(t *testing.T) {
	sec := application.CheckConfig(validSnapshot())
	if sec.Name != "config" {
		t.Errorf("section name = %q; want %q", sec.Name, "config")
	}
	if !sec.OK() {
		t.Fatalf("CheckConfig FAILed on valid snapshot: %+v", sec)
	}
}

func TestCheckConfig_MissingSecretsFail(t *testing.T) {
	snap := validSnapshot()
	snap.WebhookSecretSet = false
	snap.SlackTokenSet = false

	sec := application.CheckConfig(snap)
	if sec.OK() {
		t.Fatalf("CheckConfig succeeded with missing secrets")
	}

	want := map[string]bool{"GITHUB_WEBHOOK_SECRET": false, "SLACK_BOT_TOKEN": false}
	for _, c := range sec.Checks {
		if _, ok := want[c.Name]; ok && c.Status == validationdomain.StatusFail {
			want[c.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected FAIL for %s; not found in report: %+v", name, sec.Checks)
		}
	}
}

// TestCheckConfig_NeverPrintsSecretValues asserts that the snapshot exposes
// only booleans for secrets — the detail fields never contain a raw value.
func TestCheckConfig_NeverPrintsSecretValues(t *testing.T) {
	snap := validSnapshot()
	sec := application.CheckConfig(snap)
	for _, c := range sec.Checks {
		// Only "set" or "missing" should appear, never any raw token value.
		if c.Detail != "set" && c.Detail != "missing; set the environment variable" &&
			c.Name != "cleanup.message_ttl_days" &&
			c.Name != "database.url" &&
			c.Name != "config.yaml" &&
			c.Name != "server.domain" {
			t.Errorf("unexpected detail for %s: %q (should only be 'set' or 'missing')", c.Name, c.Detail)
		}
	}
}

func findConfigCheck(t *testing.T, sec diagnosticsdomain.Section, name string) validationdomain.CheckResult {
	t.Helper()
	for _, c := range sec.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q not found in section: %+v", name, sec.Checks)
	return validationdomain.CheckResult{}
}

func TestCheckConfig_ValidDomainReportsWebhookURL(t *testing.T) {
	snap := validSnapshot()
	snap.Domain = "notifycat.example.com"
	sec := application.CheckConfig(snap)
	c := findConfigCheck(t, sec, "server.domain")
	if c.Status != validationdomain.StatusOK {
		t.Fatalf("DOMAIN check = %+v; want OK", c)
	}
	if !strings.Contains(c.Detail, "https://notifycat.example.com/webhook/github") {
		t.Errorf("detail should name the exact webhook URL to paste into GitHub, got %q", c.Detail)
	}
}

func TestCheckConfig_DomainWithSchemeFails(t *testing.T) {
	snap := validSnapshot()
	snap.Domain = "https://notifycat.example.com"
	sec := application.CheckConfig(snap)
	c := findConfigCheck(t, sec, "server.domain")
	if c.Status != validationdomain.StatusFail {
		t.Fatalf("DOMAIN carrying a scheme should FAIL (it must be a bare host), got %+v", c)
	}
	if sec.OK() {
		t.Errorf("section must not be OK when DOMAIN is invalid")
	}
}

func TestCheckConfig_MalformedDomainFails(t *testing.T) {
	snap := validSnapshot()
	snap.Domain = "not a valid host"
	sec := application.CheckConfig(snap)
	c := findConfigCheck(t, sec, "server.domain")
	if c.Status != validationdomain.StatusFail {
		t.Fatalf("malformed DOMAIN should FAIL, got %+v", c)
	}
}

func TestCheckConfig_UnsetDomainSkips(t *testing.T) {
	snap := validSnapshot() // Domain is empty
	sec := application.CheckConfig(snap)
	c := findConfigCheck(t, sec, "server.domain")
	if c.Status != validationdomain.StatusSkip {
		t.Fatalf("unset DOMAIN should SKIP (local-dev/tunnel users), got %+v", c)
	}
	if !sec.OK() {
		t.Errorf("a SKIP must not fail the section")
	}
}

func TestCheckConfig_RejectsNonPositiveTTL(t *testing.T) {
	snap := validSnapshot()
	snap.MessageTTLDays = 0
	sec := application.CheckConfig(snap)
	if sec.OK() {
		t.Fatalf("CheckConfig should FAIL on MessageTTLDays=0")
	}
}

// ---- CheckDatabase tests (application-layer view: reads pre-computed snapshot) ----

func TestCheckDatabase_OpenableReturnsOK(t *testing.T) {
	snap := validSnapshot()
	snap.DatabaseURL = "file:/some/path/doctor.db"
	snap.DatabaseOpenable = true
	snap.DatabaseDetail = snap.DatabaseURL

	sec := application.CheckDatabase(snap)
	if sec.Name != "database" {
		t.Errorf("section name = %q; want %q", sec.Name, "database")
	}
	if !sec.OK() {
		t.Fatalf("CheckDatabase FAILed on openable snapshot: %+v", sec.Checks)
	}
}

func TestCheckDatabase_NotOpenableFails(t *testing.T) {
	snap := validSnapshot()
	snap.DatabaseURL = "file:/this/path/does/not/exist/doctor.db"
	snap.DatabaseOpenable = false
	snap.DatabaseDetail = `cannot open "file:/this/path/does/not/exist/doctor.db": store: open: ...; ensure the parent directory exists and is writable`

	sec := application.CheckDatabase(snap)
	if sec.OK() {
		t.Fatalf("CheckDatabase succeeded on not-openable snapshot: %+v", sec.Checks)
	}
}

func TestCheckDatabase_EmptyDSNFails(t *testing.T) {
	snap := validSnapshot()
	snap.DatabaseURL = ""
	snap.DatabaseOpenable = false
	snap.DatabaseDetail = ""

	sec := application.CheckDatabase(snap)
	if sec.OK() {
		t.Fatalf("CheckDatabase succeeded on empty DSN snapshot")
	}
}

// ---- CheckMappings tests ----

func oneEntrySnapshot() diagnosticsdomain.ConfigSnapshot {
	snap := validSnapshot()
	snap.Entries = []routingdomain.Entry{
		{Org: "octo", Repo: "widget", Channel: "C0123ABCDE"},
	}
	return snap
}

func TestCheckMappings_WithEntriesIsOK(t *testing.T) {
	snap := oneEntrySnapshot()
	sec := application.CheckMappings(snap)
	if sec.Name != "mappings" {
		t.Errorf("section name = %q; want %q", sec.Name, "mappings")
	}
	if !sec.OK() {
		t.Fatalf("CheckMappings FAILed on valid snapshot: %+v", sec.Checks)
	}
	if len(sec.Checks) == 0 || !strings.Contains(sec.Checks[0].Detail, "1 entries") {
		t.Errorf("Detail = %q; want it to contain \"1 entries\"", sec.Checks)
	}
}

func TestCheckMappings_EmptyMappingsIsOK(t *testing.T) {
	snap := validSnapshot()
	snap.Entries = nil
	sec := application.CheckMappings(snap)
	if !sec.OK() {
		t.Fatalf("CheckMappings FAILed on empty mappings: %+v", sec.Checks)
	}
}

func TestCheckMappings_PathRoutingActiveWithToken(t *testing.T) {
	snap := oneEntrySnapshot()
	snap.HasPathRules = true
	snap.TokenSet = true
	sec := application.CheckMappings(snap)
	c := findPathRoutingCheck(t, sec)
	if c.Status != validationdomain.StatusOK {
		t.Errorf("path routing status = %v; want OK with a token", c.Status)
	}
	if !sec.OK() {
		t.Errorf("section should be OK with token: %+v", sec.Checks)
	}
}

func TestCheckMappings_PathRoutingInertWithoutToken(t *testing.T) {
	snap := oneEntrySnapshot()
	snap.HasPathRules = true
	snap.TokenSet = false
	sec := application.CheckMappings(snap)
	c := findPathRoutingCheck(t, sec)
	if c.Status != validationdomain.StatusSkip {
		t.Errorf("path routing status = %v; want SKIP without a token", c.Status)
	}
	if !sec.OK() {
		t.Errorf("inert path routing is a SKIP, not a failure: %+v", sec.Checks)
	}
}

func findPathRoutingCheck(t *testing.T, sec diagnosticsdomain.Section) validationdomain.CheckResult {
	t.Helper()
	for _, c := range sec.Checks {
		if c.Name == "path routing" {
			return c
		}
	}
	t.Fatalf("no \"path routing\" check in section: %+v", sec.Checks)
	return validationdomain.CheckResult{}
}

// ---- Doctor.Run tests ----

func TestDoctorRun_AlwaysReturnsConfigDatabaseMappings(t *testing.T) {
	snap := validSnapshot()
	d := application.NewDoctor(snap, nil)
	sections := d.Run(context.Background(), "")

	if len(sections) != 3 {
		t.Fatalf("got %d sections; want 3 (config, database, mappings)", len(sections))
	}
	wantOrder := []string{"config", "database", "mappings"}
	for i, want := range wantOrder {
		if sections[i].Name != want {
			t.Errorf("sections[%d].Name = %q; want %q", i, sections[i].Name, want)
		}
	}
}

func TestDoctorRun_TargetRepositoryDelegatesToValidator(t *testing.T) {
	snap := validSnapshot()
	fake := &fakeRepoValidator{
		report: validationdomain.Report{
			Repository: "octo/widget",
			Checks: []validationdomain.CheckResult{
				{Name: "slack-auth", Status: validationdomain.StatusOK, Detail: "ok"},
				{Name: "slack-channel", Status: validationdomain.StatusFail, Detail: "bot not in channel"},
			},
		},
	}
	d := application.NewDoctor(snap, fake)
	sections := d.Run(context.Background(), "octo/widget")

	if fake.got != "octo/widget" {
		t.Errorf("validator.Validate called with %q; want %q", fake.got, "octo/widget")
	}
	if len(sections) != 4 {
		t.Fatalf("got %d sections; want 4 (config, database, mappings, octo/widget)", len(sections))
	}
	repoSection := sections[3]
	if repoSection.Name != "octo/widget" {
		t.Errorf("repo section name = %q; want %q", repoSection.Name, "octo/widget")
	}
	if repoSection.OK() {
		t.Errorf("repo section should reflect validator FAIL")
	}
}

func TestDoctorRun_TargetRepositoryWithoutValidatorIsNoop(t *testing.T) {
	snap := validSnapshot()
	d := application.NewDoctor(snap, nil)
	sections := d.Run(context.Background(), "octo/widget")
	if len(sections) != 3 {
		t.Fatalf("got %d sections; want 3 (no validator available, repo target ignored)", len(sections))
	}
}
