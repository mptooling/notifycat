package mappingcli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/mptooling/notifycat/internal/mappings"
	"github.com/mptooling/notifycat/internal/validate"
)

// MappingsValidator is the validate use case. main_test.go swaps it for a
// fake that satisfies this interface.
type MappingsValidator interface {
	Validate(ctx context.Context, target string, force bool, stdout, stderr io.Writer) int
}

// mappingsValidator is the production implementation.
type mappingsValidator struct {
	provider *mappings.Provider
	checker  validate.RepoValidator
	lister   validate.OrgRepoLister
	lockPath string
	clock    func() time.Time
}

// NewMappingsValidator builds the validate use case from its dependencies.
// Callers (cmd/notifycat-config, tests) construct provider/checker/lister/
// clock themselves and pass them in — there is no production-wiring façade
// in this package. `lister` may be nil when no GitHub credentials exist.
func NewMappingsValidator(
	provider *mappings.Provider,
	checker validate.RepoValidator,
	lister validate.OrgRepoLister,
	lockPath string,
	clock func() time.Time,
) MappingsValidator {
	return &mappingsValidator{provider: provider, checker: checker, lister: lister, lockPath: lockPath, clock: clock}
}

// Validate dispatches on target / force. Exit codes: 0 OK, 1 failure.
func (v *mappingsValidator) Validate(ctx context.Context, target string, force bool, stdout, stderr io.Writer) int {
	if target != "" {
		return v.runTargeted(ctx, target, stdout, stderr)
	}
	return v.runFull(ctx, force, stdout, stderr)
}

func (v *mappingsValidator) runTargeted(ctx context.Context, target string, stdout, stderr io.Writer) int {
	report := v.checker.Validate(ctx, target)
	if code := renderReports([]validate.Report{report}, stdout); code != 0 {
		return code
	}
	return v.lockExplicitEntry(target, stderr)
}

// lockExplicitEntry updates the lock for `target` only when an explicit
// entry exists. Wildcard-resolved hits don't get a per-repo lock entry
// because the wildcard org's lock atomicity would be violated.
func (v *mappingsValidator) lockExplicitEntry(target string, stderr io.Writer) int {
	entry, ok := v.findExplicitEntry(target)
	if !ok {
		return 0
	}
	lock, _ := mappings.ReadLock(v.lockPath)
	merged := mappings.MergeLock(lock,
		map[string]mappings.LockEntry{
			entry.Key(): {SHA256: entry.Hash(), ValidatedAt: v.clock()},
		}, nil)
	if err := mappings.WriteLock(v.lockPath, merged); err != nil {
		fmt.Fprintln(stderr, "validate: write lock:", err)
		return 1
	}
	return 0
}

func (v *mappingsValidator) findExplicitEntry(target string) (mappings.Entry, bool) {
	for _, e := range v.provider.Entries() {
		if !e.Wildcard && e.Key() == target {
			return e, true
		}
	}
	return mappings.Entry{}, false
}

func (v *mappingsValidator) runFull(ctx context.Context, force bool, stdout, stderr io.Writer) int {
	entries := v.provider.Entries()
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "no mappings to validate; add entries to the mappings: section of config.yaml")
		return 0
	}
	lock, toValidate, stale := v.planFull(entries, force, stderr)
	if len(toValidate) == 0 {
		fmt.Fprintln(stdout, "lock is up to date; nothing to validate")
		if len(stale) == 0 {
			return 0
		}
		return v.writeMerged(lock, nil, stale, stderr)
	}
	results := validate.RunForEntries(ctx, toValidate, v.lister, v.checker)
	code := renderReports(flattenReports(results), stdout)
	if writeErr := v.writeMerged(lock, successMap(results, v.clock), stale, stderr); writeErr != 0 && code == 0 {
		code = writeErr
	}
	return code
}

func (v *mappingsValidator) planFull(entries []mappings.Entry, force bool, stderr io.Writer) (mappings.Lock, []mappings.Entry, []string) {
	if force {
		empty := mappings.Lock{Version: mappings.LockVersion, Entries: map[string]mappings.LockEntry{}}
		return empty, entries, nil
	}
	lock, err := mappings.ReadLock(v.lockPath)
	if err != nil {
		fmt.Fprintln(stderr, "validate: warning:", err, "(rebuilding lock)")
		lock = mappings.Lock{Version: mappings.LockVersion, Entries: map[string]mappings.LockEntry{}}
	}
	diff := mappings.DiffEntries(entries, lock)
	return lock, diff.Needs, diff.Stale
}

func (v *mappingsValidator) writeMerged(lock mappings.Lock, ok map[string]mappings.LockEntry, stale []string, stderr io.Writer) int {
	merged := mappings.MergeLock(lock, ok, stale)
	if err := mappings.WriteLock(v.lockPath, merged); err != nil {
		fmt.Fprintln(stderr, "validate: write lock:", err)
		return 1
	}
	return 0
}

func successMap(results []validate.EntryResult, clock func() time.Time) map[string]mappings.LockEntry {
	out := map[string]mappings.LockEntry{}
	for _, r := range results {
		if r.OK() {
			out[r.Entry.Key()] = mappings.LockEntry{SHA256: r.Entry.Hash(), ValidatedAt: clock()}
		}
	}
	return out
}

func flattenReports(results []validate.EntryResult) []validate.Report {
	var out []validate.Report
	for _, r := range results {
		out = append(out, r.Reports...)
	}
	return out
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
