package validate

import (
	"context"
	"fmt"

	"github.com/mptooling/notifycat/internal/mappings"
)

// RepoValidator validates one repository at a time. *Validator satisfies
// it; the runner depends on this narrow surface so wildcard expansion can
// be tested without standing up real Slack/GitHub clients.
type RepoValidator interface {
	Validate(ctx context.Context, repository string) Report
}

var _ RepoValidator = (*Validator)(nil)

// EntryResult bundles every report produced for a single mapping entry,
// so callers can update the lock per-entry: an entry is "validated" only
// when every report it produced is OK.
type EntryResult struct {
	Entry   mappings.Entry
	Reports []Report
}

// OK reports whether every contributed report passed.
func (r EntryResult) OK() bool {
	for _, rep := range r.Reports {
		if !rep.OK() {
			return false
		}
	}
	return true
}

// RunForEntries validates a slice of mapping entries, expanding wildcard
// entries against lister. It never short-circuits: per-repo failures and
// lister errors surface as the entry's reports so the operator sees every
// mapping's outcome in one run.
//
// lister may be nil; wildcard entries then produce a single Skip report.
func RunForEntries(
	ctx context.Context,
	entries []mappings.Entry,
	lister OrgRepoLister,
	v RepoValidator,
) []EntryResult {
	out := make([]EntryResult, 0, len(entries))
	for _, e := range entries {
		out = append(out, EntryResult{Entry: e, Reports: reportsFor(ctx, e, lister, v)})
	}
	return out
}

func reportsFor(ctx context.Context, e mappings.Entry, lister OrgRepoLister, v RepoValidator) []Report {
	if !e.Wildcard {
		return []Report{v.Validate(ctx, e.Org+"/"+e.Repo)}
	}
	return expandWildcard(ctx, e, lister, v)
}

// expandWildcard turns one wildcard entry into per-repo reports, or a
// single status report when expansion cannot proceed.
func expandWildcard(ctx context.Context, e mappings.Entry, lister OrgRepoLister, v RepoValidator) []Report {
	key := e.Key()
	if lister == nil {
		return []Report{singleCheckReport(key, StatusSkip,
			fmt.Sprintf("no GitHub credentials configured; cannot expand %q", key))}
	}
	repos, err := lister.ListOrgRepos(ctx, e.Org)
	if err != nil {
		return []Report{singleCheckReport(key, StatusFail,
			fmt.Sprintf("list repos in %s: %v", e.Org, err))}
	}
	out := make([]Report, 0, len(repos))
	for _, r := range repos {
		out = append(out, v.Validate(ctx, e.Org+"/"+r))
	}
	return out
}

func singleCheckReport(repository string, status Status, detail string) Report {
	return Report{
		Repository: repository,
		Checks:     []CheckResult{{Name: "org-repos", Status: status, Detail: detail}},
	}
}
