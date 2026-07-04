package application

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// Doctor implements diagnosticsdomain.Doctor. It validates a ConfigSnapshot and
// delegates per-repo checks to a RepoValidator. Construct via NewDoctor.
type Doctor struct {
	snapshot  diagnosticsdomain.ConfigSnapshot
	validator validationdomain.RepoValidator
}

// NewDoctor returns a Doctor wired to snapshot and validator. validator may be
// nil — Run then skips the per-repo Slack/GitHub checks even when a target
// repository is given.
func NewDoctor(snapshot diagnosticsdomain.ConfigSnapshot, validator validationdomain.RepoValidator) *Doctor {
	return &Doctor{snapshot: snapshot, validator: validator}
}

// Run produces the report. The first three sections (config, database,
// mappings) always run, in that order. When target is non-empty and a
// validator is configured, a fourth section named after target is appended
// with the per-mapping check results.
func (d *Doctor) Run(ctx context.Context, target string) []diagnosticsdomain.Section {
	sections := []diagnosticsdomain.Section{
		CheckConfig(d.snapshot),
		CheckDatabase(d.snapshot),
		CheckMappings(d.snapshot),
	}
	if target == "" || d.validator == nil {
		return sections
	}
	report := d.validator.Validate(ctx, target)
	sections = append(sections, diagnosticsdomain.Section{Name: target, Checks: report.Checks})
	return sections
}

// CheckConfig inspects the snapshot and reports per-field results. Secret
// values are never written to Detail — the result reports only "set" or
// "missing".
func CheckConfig(snapshot diagnosticsdomain.ConfigSnapshot) diagnosticsdomain.Section {
	sec := diagnosticsdomain.Section{Name: "config"}

	if !snapshot.WebhookSecretSet {
		sec.Checks = append(sec.Checks, failResult("GITHUB_WEBHOOK_SECRET", "missing; set the environment variable"))
	} else {
		sec.Checks = append(sec.Checks, okResult("GITHUB_WEBHOOK_SECRET", "set"))
	}

	if !snapshot.SlackTokenSet {
		sec.Checks = append(sec.Checks, failResult("SLACK_BOT_TOKEN", "missing; set the environment variable"))
	} else {
		sec.Checks = append(sec.Checks, okResult("SLACK_BOT_TOKEN", "set"))
	}

	if snapshot.MessageTTLDays <= 0 {
		sec.Checks = append(sec.Checks, failResult("cleanup.message_ttl_days", "must be > 0; got %d", snapshot.MessageTTLDays))
	} else {
		sec.Checks = append(sec.Checks, okResult("cleanup.message_ttl_days", fmt.Sprintf("%d days", snapshot.MessageTTLDays)))
	}

	if snapshot.DatabaseURL == "" {
		sec.Checks = append(sec.Checks, failResult("database.url", "missing"))
	} else {
		sec.Checks = append(sec.Checks, okResult("database.url", snapshot.DatabaseURL))
	}

	if snapshot.ConfigFile == "" {
		sec.Checks = append(sec.Checks, failResult("config.yaml", "missing"))
	} else {
		sec.Checks = append(sec.Checks, okResult("config.yaml", snapshot.ConfigFile))
	}

	sec.Checks = append(sec.Checks, publicWebhookURLCheck(snapshot.Domain))
	return sec
}

// CheckDatabase reports whether the database described in the snapshot is
// reachable. The actual open+ping is performed by the infrastructure layer when
// building the snapshot; this function reads the pre-computed result.
func CheckDatabase(snapshot diagnosticsdomain.ConfigSnapshot) diagnosticsdomain.Section {
	sec := diagnosticsdomain.Section{Name: "database"}
	if snapshot.DatabaseURL == "" {
		sec.Checks = append(sec.Checks, failResult("open", "database.url is empty; set it in config.yaml to a SQLite path or file: DSN"))
		return sec
	}
	if !snapshot.DatabaseOpenable {
		sec.Checks = append(sec.Checks, failResult("open", "%s", snapshot.DatabaseDetail))
		return sec
	}
	sec.Checks = append(sec.Checks, okResult("open", snapshot.DatabaseURL))
	return sec
}

// CheckMappings reports whether the mappings section parsed into any entries.
// An empty section is OK (the server boots but routes nothing). When any tier
// configures per-path routing, it adds a "path routing" check: OK when a
// GitHub token is present (paths are active), SKIP when it is absent (path
// rules are inert — PRs route to the repo tier — until a token is set).
func CheckMappings(snapshot diagnosticsdomain.ConfigSnapshot) diagnosticsdomain.Section {
	sec := diagnosticsdomain.Section{Name: "mappings"}
	entries := snapshot.Entries
	if len(entries) == 0 {
		sec.Checks = append(sec.Checks, okResult("entries", "0 entries (server will boot but route nothing)"))
		return sec
	}
	sec.Checks = append(sec.Checks, okResult("entries", fmt.Sprintf("%d entries", len(entries))))
	if snapshot.HasPathRules {
		if snapshot.GitHubTokenSet {
			sec.Checks = append(sec.Checks, okResult("path routing", "active (GITHUB_TOKEN set)"))
		} else {
			sec.Checks = append(sec.Checks, skip("path routing",
				"GITHUB_TOKEN unset; path rules are inert — PRs route to the repo tier until a token is set"))
		}
	}
	return sec
}

// publicWebhookURLCheck validates server.domain and reports the exact URL the
// operator pastes into the GitHub webhook. The most common install-path mistake
// is putting a scheme or path in the value, or leaving it as a bare host that
// doesn't parse — both FAIL here with a remediation hint. When server.domain is
// unset the check is a SKIP, not a FAIL: local-dev and tunnel (ngrok) users
// legitimately have no fixed public host.
func publicWebhookURLCheck(domain string) validationdomain.CheckResult {
	const name = "server.domain"
	d := strings.TrimSpace(domain)
	if d == "" {
		return skip(name, "not set — skipping the public webhook URL check (expected for local dev / tunnels; "+
			"set server.domain in config.yaml to your public host, e.g. notifycat.example.com, to enable it)")
	}
	if strings.Contains(d, "://") {
		return failResult(name, "must be a bare host like notifycat.example.com, not a full URL: got %q", d)
	}
	u, err := url.Parse("https://" + d + "/webhook/github")
	if err != nil || u.Host != d {
		return failResult(name, "not a valid host: %q — use a bare hostname like notifycat.example.com", d)
	}
	return okResult(name, "paste this into the GitHub webhook Payload URL: "+u.String())
}

func okResult(name, detail string) validationdomain.CheckResult {
	return validationdomain.CheckResult{Name: name, Status: validationdomain.StatusOK, Detail: detail}
}

func failResult(name, format string, args ...any) validationdomain.CheckResult {
	return validationdomain.CheckResult{
		Name:   name,
		Status: validationdomain.StatusFail,
		Detail: fmt.Sprintf(format, args...),
	}
}

func skip(name, detail string) validationdomain.CheckResult {
	return validationdomain.CheckResult{Name: name, Status: validationdomain.StatusSkip, Detail: detail}
}
