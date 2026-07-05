package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	validationapp "github.com/mptooling/notifycat/internal/validation/application"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// List prints the current mapping entries as a tab-separated table to stdout
// and returns exit code 0. It satisfies the `notifycat-config list` command.
func List(entries diagnosticsdomain.EntrySource, stdout io.Writer) int {
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ORG\tREPO\tCHANNEL\tMENTIONS")
	for _, entry := range entries.Entries() {
		repo := entry.Repo
		if entry.Wildcard {
			repo = "*"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			entry.Org, repo, entry.Channel, strings.Join(entry.Mentions, ","))
	}
	_ = tw.Flush()
	return 0
}

// MappingsValidator is the validate use case. Callers (cmd/notifycat-config,
// tests) inject the ports; there is no production-wiring façade.
type MappingsValidator struct {
	entries diagnosticsdomain.EntrySource
	checker validationdomain.RepoValidator
	lister  validationdomain.RepoLister
	gateway diagnosticsdomain.LockGateway
}

// NewMappingsValidator builds the validate use case from its dependencies.
// lister may be nil when no provider credentials exist.
func NewMappingsValidator(
	entries diagnosticsdomain.EntrySource,
	checker validationdomain.RepoValidator,
	lister validationdomain.RepoLister,
	gateway diagnosticsdomain.LockGateway,
) *MappingsValidator {
	return &MappingsValidator{entries: entries, checker: checker, lister: lister, gateway: gateway}
}

// Validate dispatches on target / force. Exit codes: 0 OK, 1 failure.
func (v *MappingsValidator) Validate(ctx context.Context, target string, force bool, stdout, stderr io.Writer) int {
	if target != "" {
		return v.runTargeted(ctx, target, stdout, stderr)
	}
	return v.runFull(ctx, force, stdout, stderr)
}

func (v *MappingsValidator) runTargeted(ctx context.Context, target string, stdout, stderr io.Writer) int {
	report := v.checker.Validate(ctx, target)
	if code := renderReports([]validationdomain.Report{report}, stdout); code != 0 {
		return code
	}
	return v.lockExplicitEntry(target, stderr)
}

// lockExplicitEntry updates the lock for target only when an explicit entry
// exists. Wildcard-resolved hits don't get a per-repo lock entry because the
// wildcard org's lock atomicity would be violated.
func (v *MappingsValidator) lockExplicitEntry(target string, stderr io.Writer) int {
	entry, ok := v.findExplicitEntry(target)
	if !ok {
		return 0
	}
	if err := v.gateway.CommitTargeted(entry); err != nil {
		fmt.Fprintln(stderr, "validate: write lock:", err)
		return 1
	}
	return 0
}

func (v *MappingsValidator) findExplicitEntry(target string) (routingdomain.Entry, bool) {
	for _, entry := range v.entries.Entries() {
		if !entry.Wildcard && entry.Key() == target {
			return entry, true
		}
	}
	return routingdomain.Entry{}, false
}

func (v *MappingsValidator) runFull(ctx context.Context, force bool, stdout, stderr io.Writer) int {
	allEntries := v.entries.Entries()
	if len(allEntries) == 0 {
		fmt.Fprintln(stdout, "no mappings to validate; add entries to the mappings: section of config.yaml")
		return 0
	}
	plan, planErr := v.gateway.Plan(allEntries, force)
	if planErr != nil {
		fmt.Fprintln(stderr, "validate: warning:", planErr, "(rebuilding lock)")
	}
	if len(plan.ToValidate) == 0 {
		fmt.Fprintln(stdout, "lock is up to date; nothing to validate")
		if len(plan.Stale) == 0 {
			return 0
		}
		if err := v.gateway.Commit(nil, plan.Stale); err != nil {
			fmt.Fprintln(stderr, "validate: write lock:", err)
			return 1
		}
		return 0
	}
	results := validationapp.RunForEntries(ctx, plan.ToValidate, v.lister, v.checker)
	code := renderReports(flattenReports(results), stdout)
	if err := v.gateway.Commit(results, plan.Stale); err != nil {
		fmt.Fprintln(stderr, "validate: write lock:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

func flattenReports(results []validationdomain.EntryResult) []validationdomain.Report {
	var out []validationdomain.Report
	for _, result := range results {
		out = append(out, result.Reports...)
	}
	return out
}

func renderReports(reports []validationdomain.Report, stdout io.Writer) int {
	allOK := true
	for reportIndex, report := range reports {
		if reportIndex > 0 {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "%s\n", report.Repository)
		for _, check := range report.Checks {
			fmt.Fprintf(stdout, "  %-4s  %-16s  %s\n", check.Status, check.Name, check.Detail)
		}
		if !report.OK() {
			allOK = false
		}
	}
	if !allOK {
		return 1
	}
	return 0
}
