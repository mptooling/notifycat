// Package doctor runs end-to-end preflight diagnostics for a notifycat
// installation. It bundles config, database, and mappings-file checks the
// server would otherwise only surface at startup, and (for a target
// repository) delegates the per-mapping Slack + GitHub checks to
// internal/validate. The CLI entry point lives in cmd/notifycat-doctor.
//
// Orchestration lives here; each section's checks live in their own
// *_check.go file (config_check.go, database_check.go, mappings_check.go),
// and the okResult/failResult/skip constructors live in helpers.go — the
// same layout the sibling internal/validate package uses.
package doctor

import (
	"context"
	"fmt"
	"io"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/mappings"
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
	provider := mappings.NewProvider(mappings.Defaults{}, d.cfg.Mappings, d.cfg.Digest)
	sections := []Section{
		CheckConfig(d.cfg),
		CheckDatabase(d.cfg.DatabaseURL),
		CheckMappings(provider),
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
