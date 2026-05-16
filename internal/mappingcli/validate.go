package mappingcli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/github"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
	"github.com/mptooling/notifycat/internal/validate"
)

// MappingsValidator is the validate use case. The cmd binary holds an
// instance; tests in main_test.go swap it for a fake that satisfies this
// interface.
type MappingsValidator interface {
	Validate(ctx context.Context, target string, stdout, stderr io.Writer) int
}

// Checker is the narrow validation surface the use case needs.
// *validate.Validator satisfies it; in-package tests inject a stub here.
type Checker interface {
	Validate(ctx context.Context, repository string) validate.Report
	ValidateAll(ctx context.Context) ([]validate.Report, error)
}

// mappingsValidator is the production implementation of MappingsValidator.
type mappingsValidator struct {
	checker Checker
}

// NewMappingsValidator wires the production validator from cfg, including
// Slack and (optionally) GitHub clients.
func NewMappingsValidator(repo *store.RepoMappings, cfg config.Config) MappingsValidator {
	hc := &http.Client{Timeout: 10 * time.Second}
	s := slack.NewClient(hc, cfg.SlackBotToken.Reveal(), slack.WithBaseURL(cfg.SlackBaseURL))
	var gh validate.GitHubChecker
	if cfg.GitHubToken.Reveal() != "" {
		gh = github.NewClient(hc, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
	}
	return newMappingsValidator(validate.NewValidator(repo, s, gh))
}

// newMappingsValidator is the package-internal constructor tests use to
// wrap a stub Checker without touching real clients.
func newMappingsValidator(c Checker) *mappingsValidator {
	return &mappingsValidator{checker: c}
}

// Validate runs validation for target (empty = all mappings), renders the
// report(s), and returns the CLI exit code: 0 OK, 1 failure.
func (v *mappingsValidator) Validate(ctx context.Context, target string, stdout, stderr io.Writer) int {
	reports, code := v.collect(ctx, target, stdout, stderr)
	if reports == nil {
		return code
	}
	return renderReports(reports, stdout)
}

func (v *mappingsValidator) collect(ctx context.Context, target string, stdout, stderr io.Writer) ([]validate.Report, int) {
	if target == "" {
		reports, err := v.checker.ValidateAll(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "validate:", err)
			return nil, 1
		}
		if len(reports) == 0 {
			fmt.Fprintln(stdout, "no mappings to validate; add one with `notifycat-mapping add`")
			return nil, 0
		}
		return reports, 0
	}
	return []validate.Report{v.checker.Validate(ctx, target)}, 0
}

func renderReports(reports []validate.Report, stdout io.Writer) int {
	allOK := true
	for i, r := range reports {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "%s\n", r.Repository)
		for _, c := range r.Checks {
			fmt.Fprintf(stdout, "  %-4s  %-16s  %s\n", c.Status, c.Name, c.Detail)
		}
		if !r.OK() {
			allOK = false
		}
	}
	if !allOK {
		return 1
	}
	return 0
}
