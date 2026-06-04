// Package doctor runs end-to-end preflight diagnostics for a notifycat
// installation. It bundles config, database, and mappings-file checks the
// server would otherwise only surface at startup, and (for a target
// repository) delegates the per-mapping Slack + GitHub checks to
// internal/validate. The CLI entry point lives in cmd/notifycat-doctor.
package doctor

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/mappings"
	"github.com/mptooling/notifycat/internal/store"
	"github.com/mptooling/notifycat/internal/validate"
)

// RepoValidator is the slice of validate.Validator the doctor needs. It
// stays in this consumer package so tests can supply a hand-written fake
// without depending on the live Slack / GitHub clients.
type RepoValidator interface {
	Validate(ctx context.Context, repository string) validate.Report
}

// Doctor bundles a parsed config with an optional RepoValidator. Construct
// once via NewDoctor; Run is safe to call multiple times.
type Doctor struct {
	cfg       config.Config
	validator RepoValidator
}

// NewDoctor returns a Doctor wired to cfg and validator. validator may be
// nil — Run then skips the per-repo Slack/GitHub checks even when a target
// repository is given.
func NewDoctor(cfg config.Config, validator RepoValidator) *Doctor {
	return &Doctor{cfg: cfg, validator: validator}
}

// Run produces the report. The first three sections (config, database,
// mappings) always run, in that order. When target is non-empty and a
// validator is configured, a fourth section named after target is appended
// with the per-mapping check results.
func (d *Doctor) Run(ctx context.Context, target string) []Section {
	sections := []Section{
		CheckConfig(d.cfg),
		CheckDatabase(d.cfg.DatabaseURL),
		CheckMappingsFile(d.cfg.MappingsFile),
	}
	if target == "" || d.validator == nil {
		return sections
	}
	report := d.validator.Validate(ctx, target)
	sections = append(sections, Section{Name: target, Checks: report.Checks})
	return sections
}

// Section is a named group of related checks (e.g. "config", "database",
// "octo/widget"). Each section's checks are independent — a single FAIL in
// one section does not short-circuit other sections.
type Section struct {
	Name   string
	Checks []validate.CheckResult
}

// OK reports whether every check in the section passed (StatusSkip does not
// count as a failure).
func (s Section) OK() bool {
	for _, c := range s.Checks {
		if c.Status == validate.StatusFail {
			return false
		}
	}
	return true
}

// CheckConfig inspects cfg and reports per-field results. Secret values are
// never written to Detail — the result reports only "set" or "missing".
func CheckConfig(cfg config.Config) Section {
	sec := Section{Name: "config"}
	sec.Checks = append(sec.Checks, secretCheck("GITHUB_WEBHOOK_SECRET", cfg.GitHubWebhookSecret))
	sec.Checks = append(sec.Checks, secretCheck("SLACK_BOT_TOKEN", cfg.SlackBotToken))
	if cfg.MessageTTLDays <= 0 {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "NOTIFYCAT_MESSAGE_TTL_DAYS",
			Status: validate.StatusFail,
			Detail: fmt.Sprintf("must be > 0; got %d", cfg.MessageTTLDays),
		})
	} else {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "NOTIFYCAT_MESSAGE_TTL_DAYS",
			Status: validate.StatusOK,
			Detail: fmt.Sprintf("%d days", cfg.MessageTTLDays),
		})
	}
	if cfg.DatabaseURL == "" {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "DATABASE_URL",
			Status: validate.StatusFail,
			Detail: "missing",
		})
	} else {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "DATABASE_URL",
			Status: validate.StatusOK,
			Detail: cfg.DatabaseURL,
		})
	}
	if cfg.MappingsFile == "" {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "NOTIFYCAT_MAPPINGS_FILE",
			Status: validate.StatusFail,
			Detail: "missing",
		})
	} else {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "NOTIFYCAT_MAPPINGS_FILE",
			Status: validate.StatusOK,
			Detail: cfg.MappingsFile,
		})
	}
	sec.Checks = append(sec.Checks, publicWebhookURLCheck(cfg.Domain))
	return sec
}

// publicWebhookURLCheck validates DOMAIN and reports the exact URL the operator
// pastes into the GitHub webhook. DOMAIN is the single source of truth for the
// public host (the docker-compose reverse proxy reads the same value), so the
// doctor derives https://$DOMAIN/webhook/github rather than asking for the URL
// separately. The most common install-path mistake is putting a scheme or path
// in DOMAIN, or leaving it as a bare host that doesn't parse — both FAIL here
// with a remediation hint. When DOMAIN is unset the check is a SKIP, not a FAIL:
// local-dev and tunnel (ngrok) users legitimately have no fixed public host.
func publicWebhookURLCheck(domain string) validate.CheckResult {
	const name = "DOMAIN"
	d := strings.TrimSpace(domain)
	if d == "" {
		return validate.CheckResult{
			Name:   name,
			Status: validate.StatusSkip,
			Detail: "not set — skipping the public webhook URL check (expected for local dev / tunnels; " +
				"set DOMAIN to your public host, e.g. notifycat.example.com, in .env or the environment to enable it)",
		}
	}
	if strings.Contains(d, "://") {
		return validate.CheckResult{
			Name:   name,
			Status: validate.StatusFail,
			Detail: fmt.Sprintf("must be a bare host like notifycat.example.com, not a full URL: got %q", d),
		}
	}
	u, err := url.Parse("https://" + d + "/webhook/github")
	if err != nil || u.Host != d {
		return validate.CheckResult{
			Name:   name,
			Status: validate.StatusFail,
			Detail: fmt.Sprintf("not a valid host: %q — use a bare hostname like notifycat.example.com", d),
		}
	}
	return validate.CheckResult{
		Name:   name,
		Status: validate.StatusOK,
		Detail: "paste this into the GitHub webhook Payload URL: " + u.String(),
	}
}

// CheckDatabase opens dsn, pings the underlying connection, and reports the
// result. It does not run migrations — that is the server's job.
func CheckDatabase(dsn string) Section {
	sec := Section{Name: "database"}
	if dsn == "" {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "open",
			Status: validate.StatusFail,
			Detail: "DATABASE_URL is empty; set it to a SQLite path or file: DSN",
		})
		return sec
	}
	db, err := store.Open(dsn)
	if err != nil {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "open",
			Status: validate.StatusFail,
			Detail: fmt.Sprintf("cannot open %q: %v; ensure the parent directory exists and is writable", dsn, err),
		})
		return sec
	}
	sqlDB, err := store.SQLDB(db)
	if err != nil {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "open",
			Status: validate.StatusFail,
			Detail: fmt.Sprintf("cannot resolve underlying *sql.DB: %v", err),
		})
		return sec
	}
	defer func() { _ = sqlDB.Close() }()
	if err := sqlDB.Ping(); err != nil {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "ping",
			Status: validate.StatusFail,
			Detail: fmt.Sprintf("ping failed: %v", err),
		})
		return sec
	}
	sec.Checks = append(sec.Checks, validate.CheckResult{
		Name:   "open",
		Status: validate.StatusOK,
		Detail: dsn,
	})
	return sec
}

// CheckMappingsFile loads the mappings file via internal/mappings.Load and
// reports whether the file exists and parses cleanly. An empty mappings map
// is OK (the server treats it as a no-op).
func CheckMappingsFile(path string) Section {
	sec := Section{Name: "mappings"}
	if path == "" {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "file",
			Status: validate.StatusFail,
			Detail: "NOTIFYCAT_MAPPINGS_FILE is empty; set it or rely on the ./mappings.yaml default",
		})
		return sec
	}
	provider, err := mappings.Load(path)
	if err != nil {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "file",
			Status: validate.StatusFail,
			Detail: fmt.Sprintf("cannot load %q: %v", path, err),
		})
		return sec
	}
	sec.Checks = append(sec.Checks, validate.CheckResult{
		Name:   "file",
		Status: validate.StatusOK,
		Detail: path,
	})
	entries := provider.Entries()
	if len(entries) == 0 {
		sec.Checks = append(sec.Checks, validate.CheckResult{
			Name:   "entries",
			Status: validate.StatusOK,
			Detail: "0 entries (server will boot but route nothing)",
		})
		return sec
	}
	sec.Checks = append(sec.Checks, validate.CheckResult{
		Name:   "entries",
		Status: validate.StatusOK,
		Detail: fmt.Sprintf("%d entries", len(entries)),
	})
	return sec
}

func secretCheck(name string, s config.Secret) validate.CheckResult {
	if s.Reveal() == "" {
		return validate.CheckResult{
			Name:   name,
			Status: validate.StatusFail,
			Detail: "missing; set the environment variable",
		}
	}
	return validate.CheckResult{Name: name, Status: validate.StatusOK, Detail: "set"}
}

// WriteReport renders sections to w in a human-readable, greppable form,
// and returns true iff no check failed (skipped checks do not fail the
// report). The format is intentionally plain text: one section header per
// group, then one indented line per check.
func WriteReport(w io.Writer, sections []Section) bool {
	allOK := true
	for _, sec := range sections {
		fmt.Fprintf(w, "[%s]\n", sec.Name)
		for _, c := range sec.Checks {
			if c.Status == validate.StatusFail {
				allOK = false
			}
			if c.Detail == "" {
				fmt.Fprintf(w, "  %-4s  %s\n", c.Status, c.Name)
				continue
			}
			fmt.Fprintf(w, "  %-4s  %s — %s\n", c.Status, c.Name, c.Detail)
		}
	}
	return allOK
}
