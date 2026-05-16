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

// Validator is the narrow surface Validate needs. Tests inject a stub;
// production callers use NewProductionValidator.
type Validator interface {
	Validate(ctx context.Context, repository string) validate.Report
	ValidateAll(ctx context.Context) ([]validate.Report, error)
}

// ValidatorFactory builds a Validator for one Validate invocation.
type ValidatorFactory func(repo *store.RepoMappings) (Validator, error)

// NewProductionValidator wires real Slack and (optionally) GitHub clients.
func NewProductionValidator(cfg config.Config) ValidatorFactory {
	return func(repo *store.RepoMappings) (Validator, error) {
		hc := &http.Client{Timeout: 10 * time.Second}
		s := slack.NewClient(hc, cfg.SlackBotToken.Reveal(), slack.WithBaseURL(cfg.SlackBaseURL))
		var gh validate.GitHubChecker
		if cfg.GitHubToken.Reveal() != "" {
			gh = github.NewClient(hc, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
		}
		return validate.NewValidator(repo, s, gh), nil
	}
}

// Validate runs validation for target (empty means all mappings) and renders
// the reports. Exit codes: 0 OK, 1 failure, 2 misuse.
func Validate(
	ctx context.Context,
	repo *store.RepoMappings,
	target string,
	newValidator ValidatorFactory,
	stdout, stderr io.Writer,
) int {
	v, code := buildValidator(repo, newValidator, stderr)
	if v == nil {
		return code
	}
	reports, code := collectReports(ctx, v, target, stdout, stderr)
	if reports == nil {
		return code
	}
	return renderReports(reports, stdout)
}

func buildValidator(repo *store.RepoMappings, newValidator ValidatorFactory, stderr io.Writer) (Validator, int) {
	if newValidator == nil {
		fmt.Fprintln(stderr, "validate: not configured (missing validator wiring)")
		return nil, 1
	}
	v, err := newValidator(repo)
	if err != nil {
		fmt.Fprintln(stderr, "validate:", err)
		return nil, 1
	}
	return v, 0
}

func collectReports(ctx context.Context, v Validator, target string, stdout, stderr io.Writer) ([]validate.Report, int) {
	if target == "" {
		reports, err := v.ValidateAll(ctx)
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
	return []validate.Report{v.Validate(ctx, target)}, 0
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
