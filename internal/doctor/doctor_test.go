package doctor_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/doctor"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	routinginfra "github.com/mptooling/notifycat/internal/routing/infrastructure"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

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

func validConfig() config.Config {
	return config.Config{
		Addr:                ":8080",
		DatabaseURL:         "file:./data/notifycat.db",
		ConfigFile:          "./config.yaml",
		MessageTTLDays:      30,
		GitHubWebhookSecret: config.Secret("topsecret-wh"),
		SlackBotToken:       config.Secret("xoxb-secret-token"),
	}
}

func TestWriteReport_AllOK_ReturnsTrue(t *testing.T) {
	sections := []doctor.Section{
		{Name: "config", Checks: []validationdomain.CheckResult{
			{Name: "GITHUB_WEBHOOK_SECRET", Status: validationdomain.StatusOK, Detail: "set"},
			{Name: "SLACK_BOT_TOKEN", Status: validationdomain.StatusOK, Detail: "set"},
		}},
	}
	var buf bytes.Buffer
	ok := doctor.WriteReport(&buf, sections)
	if !ok {
		t.Fatalf("WriteReport returned false for all-OK sections")
	}
	out := buf.String()
	if !strings.Contains(out, "config") {
		t.Errorf("output missing section name: %q", out)
	}
	if !strings.Contains(out, "OK") {
		t.Errorf("output missing OK status: %q", out)
	}
}

func TestWriteReport_AnyFail_ReturnsFalse(t *testing.T) {
	sections := []doctor.Section{
		{Name: "config", Checks: []validationdomain.CheckResult{
			{Name: "GITHUB_WEBHOOK_SECRET", Status: validationdomain.StatusOK, Detail: "set"},
		}},
		{Name: "database", Checks: []validationdomain.CheckResult{
			{Name: "open", Status: validationdomain.StatusFail, Detail: "no such file: /missing/path.db"},
		}},
	}
	var buf bytes.Buffer
	ok := doctor.WriteReport(&buf, sections)
	if ok {
		t.Fatalf("WriteReport returned true despite a FAIL check")
	}
	out := buf.String()
	if !strings.Contains(out, "FAIL") {
		t.Errorf("output missing FAIL status: %q", out)
	}
	if !strings.Contains(out, "no such file: /missing/path.db") {
		t.Errorf("output missing FAIL detail (remediation hint): %q", out)
	}
}

func TestWriteReport_SkipDoesNotFailOverall(t *testing.T) {
	sections := []doctor.Section{
		{Name: "github", Checks: []validationdomain.CheckResult{
			{Name: "webhook-events", Status: validationdomain.StatusSkip, Detail: "GITHUB_TOKEN not set"},
		}},
	}
	var buf bytes.Buffer
	ok := doctor.WriteReport(&buf, sections)
	if !ok {
		t.Fatalf("WriteReport returned false for sections containing only SKIPs")
	}
	if !strings.Contains(buf.String(), "SKIP") {
		t.Errorf("output missing SKIP status: %q", buf.String())
	}
}

func TestWriteReport_EmptySections(t *testing.T) {
	var buf bytes.Buffer
	if !doctor.WriteReport(&buf, nil) {
		t.Errorf("WriteReport(nil) returned false; want true (no failures)")
	}
}

func TestCheckConfig_AllSetReturnsOK(t *testing.T) {
	sec := doctor.CheckConfig(validConfig())
	if sec.Name != "config" {
		t.Errorf("section name = %q; want %q", sec.Name, "config")
	}
	if !sec.OK() {
		t.Fatalf("CheckConfig FAILed on valid config: %+v", sec)
	}
}

func TestCheckConfig_MissingSecretsFail(t *testing.T) {
	cfg := validConfig()
	cfg.GitHubWebhookSecret = ""
	cfg.SlackBotToken = ""

	sec := doctor.CheckConfig(cfg)
	if sec.OK() {
		t.Fatalf("CheckConfig succeeded with empty secrets")
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

func TestCheckConfig_NeverPrintsSecretValues(t *testing.T) {
	cfg := validConfig()
	sec := doctor.CheckConfig(cfg)
	for _, c := range sec.Checks {
		if strings.Contains(c.Detail, "topsecret-wh") || strings.Contains(c.Detail, "xoxb-secret-token") {
			t.Fatalf("secret value leaked into check detail for %s: %q", c.Name, c.Detail)
		}
	}
}

func findConfigCheck(t *testing.T, sec doctor.Section, name string) validationdomain.CheckResult {
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
	cfg := validConfig()
	cfg.Domain = "notifycat.example.com"
	sec := doctor.CheckConfig(cfg)
	c := findConfigCheck(t, sec, "server.domain")
	if c.Status != validationdomain.StatusOK {
		t.Fatalf("DOMAIN check = %+v; want OK", c)
	}
	if !strings.Contains(c.Detail, "https://notifycat.example.com/webhook/github") {
		t.Errorf("detail should name the exact webhook URL to paste into GitHub, got %q", c.Detail)
	}
}

func TestCheckConfig_DomainWithSchemeFails(t *testing.T) {
	cfg := validConfig()
	cfg.Domain = "https://notifycat.example.com"
	sec := doctor.CheckConfig(cfg)
	c := findConfigCheck(t, sec, "server.domain")
	if c.Status != validationdomain.StatusFail {
		t.Fatalf("DOMAIN carrying a scheme should FAIL (it must be a bare host), got %+v", c)
	}
	if sec.OK() {
		t.Errorf("section must not be OK when DOMAIN is invalid")
	}
}

func TestCheckConfig_MalformedDomainFails(t *testing.T) {
	cfg := validConfig()
	cfg.Domain = "not a valid host"
	sec := doctor.CheckConfig(cfg)
	c := findConfigCheck(t, sec, "server.domain")
	if c.Status != validationdomain.StatusFail {
		t.Fatalf("malformed DOMAIN should FAIL, got %+v", c)
	}
}

func TestCheckConfig_UnsetDomainSkips(t *testing.T) {
	cfg := validConfig() // Domain is empty
	sec := doctor.CheckConfig(cfg)
	c := findConfigCheck(t, sec, "server.domain")
	if c.Status != validationdomain.StatusSkip {
		t.Fatalf("unset DOMAIN should SKIP (local-dev/tunnel users), got %+v", c)
	}
	if !sec.OK() {
		t.Errorf("a SKIP must not fail the section")
	}
}

func TestCheckConfig_RejectsNonPositiveTTL(t *testing.T) {
	cfg := validConfig()
	cfg.MessageTTLDays = 0
	sec := doctor.CheckConfig(cfg)
	if sec.OK() {
		t.Fatalf("CheckConfig should FAIL on MessageTTLDays=0")
	}
}

func TestCheckDatabase_OpenableReturnsOK(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "doctor.db")
	sec := doctor.CheckDatabase(dsn)
	if sec.Name != "database" {
		t.Errorf("section name = %q; want %q", sec.Name, "database")
	}
	if !sec.OK() {
		t.Fatalf("CheckDatabase FAILed on writable path: %+v", sec.Checks)
	}
}

func TestCheckDatabase_UnreachablePathFails(t *testing.T) {
	// SQLite refuses to create a database when the parent directory does not exist.
	dsn := "file:/this/path/does/not/exist/doctor.db"
	sec := doctor.CheckDatabase(dsn)
	if sec.OK() {
		t.Fatalf("CheckDatabase succeeded on unreachable DSN: %+v", sec.Checks)
	}
}

func TestCheckDatabase_EmptyDSNFails(t *testing.T) {
	sec := doctor.CheckDatabase("")
	if sec.OK() {
		t.Fatalf("CheckDatabase succeeded on empty DSN")
	}
}

func TestCheckMappings_WithEntriesIsOK(t *testing.T) {
	m := map[string]routingdomain.Org{
		"octo": {"widget": {Channel: "C0123ABCDE"}},
	}
	sec := doctor.CheckMappings(routingapp.NewProvider(routingdomain.Defaults{}, m, nil), false)
	if sec.Name != "mappings" {
		t.Errorf("section name = %q; want %q", sec.Name, "mappings")
	}
	if !sec.OK() {
		t.Fatalf("CheckMappings FAILed on valid provider: %+v", sec.Checks)
	}
	if len(sec.Checks) == 0 || !strings.Contains(sec.Checks[0].Detail, "1 entries") {
		t.Errorf("Detail = %q; want it to contain \"1 entries\"", sec.Checks)
	}
}

func TestCheckMappings_EmptyMappingsIsOK(t *testing.T) {
	sec := doctor.CheckMappings(routingapp.NewProvider(routingdomain.Defaults{}, nil, nil), false)
	if !sec.OK() {
		t.Fatalf("CheckMappings FAILed on empty mappings (which the server treats as a no-op): %+v", sec.Checks)
	}
}

func pathRoutingProvider(t *testing.T) *routingapp.Provider {
	t.Helper()
	doc := "mappings:\n  acme:\n    mono:\n      channel: C0BASE00000\n      paths:\n        \"/src\": {mentions: []}\n"
	f, err := routinginfra.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return routingapp.NewProvider(routingdomain.Defaults{}, f.Mappings, nil)
}

func pathRoutingCheck(t *testing.T, sec doctor.Section) validationdomain.CheckResult {
	t.Helper()
	for _, c := range sec.Checks {
		if c.Name == "path routing" {
			return c
		}
	}
	t.Fatalf("no \"path routing\" check in section: %+v", sec.Checks)
	return validationdomain.CheckResult{}
}

func TestCheckMappings_PathRoutingActiveWithToken(t *testing.T) {
	sec := doctor.CheckMappings(pathRoutingProvider(t), true)
	if c := pathRoutingCheck(t, sec); c.Status != validationdomain.StatusOK {
		t.Errorf("path routing status = %v; want OK with a token", c.Status)
	}
	if !sec.OK() {
		t.Errorf("section should be OK with token: %+v", sec.Checks)
	}
}

func TestCheckMappings_PathRoutingInertWithoutToken(t *testing.T) {
	sec := doctor.CheckMappings(pathRoutingProvider(t), false)
	c := pathRoutingCheck(t, sec)
	if c.Status != validationdomain.StatusSkip {
		t.Errorf("path routing status = %v; want SKIP without a token", c.Status)
	}
	if !sec.OK() {
		t.Errorf("inert path routing is a SKIP, not a failure: %+v", sec.Checks)
	}
}

func TestDoctorRun_AlwaysReturnsConfigDatabaseMappings(t *testing.T) {
	cfg := validConfig()
	cfg.DatabaseURL = "file:" + filepath.Join(t.TempDir(), "doctor.db")

	d := doctor.NewDoctor(cfg, nil)
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
	cfg := validConfig()
	cfg.DatabaseURL = "file:" + filepath.Join(t.TempDir(), "doctor.db")

	fake := &fakeRepoValidator{
		report: validationdomain.Report{
			Repository: "octo/widget",
			Checks: []validationdomain.CheckResult{
				{Name: "slack-auth", Status: validationdomain.StatusOK, Detail: "ok"},
				{Name: "slack-channel", Status: validationdomain.StatusFail, Detail: "bot not in channel"},
			},
		},
	}
	d := doctor.NewDoctor(cfg, fake)
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
	cfg := validConfig()
	cfg.DatabaseURL = "file:" + filepath.Join(t.TempDir(), "doctor.db")

	d := doctor.NewDoctor(cfg, nil)
	sections := d.Run(context.Background(), "octo/widget")
	if len(sections) != 3 {
		t.Fatalf("got %d sections; want 3 (no validator available, repo target ignored)", len(sections))
	}
}
