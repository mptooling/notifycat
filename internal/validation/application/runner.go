package application

import (
	"context"
	"fmt"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

// RunForEntries validates a slice of mapping entries, expanding wildcard
// entries against lister. It never short-circuits: per-repo failures and lister
// errors surface as the entry's reports so the operator sees every mapping's
// outcome in one run.
//
// lister may be nil; wildcard entries then produce a single Skip report.
func RunForEntries(
	ctx context.Context,
	entries []routingdomain.Entry,
	lister domain.OrgRepoLister,
	v domain.RepoValidator,
) []domain.EntryResult {
	out := make([]domain.EntryResult, 0, len(entries))
	for _, e := range entries {
		out = append(out, domain.EntryResult{Entry: e, Reports: reportsFor(ctx, e, lister, v)})
	}
	return out
}

func reportsFor(ctx context.Context, e routingdomain.Entry, lister domain.OrgRepoLister, v domain.RepoValidator) []domain.Report {
	if !e.Wildcard {
		return []domain.Report{v.Validate(ctx, e.Org+"/"+e.Repo)}
	}
	return expandWildcard(ctx, e, lister, v)
}

// expandWildcard turns one wildcard entry into per-repo reports, or a single
// status report when expansion cannot proceed.
func expandWildcard(ctx context.Context, e routingdomain.Entry, lister domain.OrgRepoLister, v domain.RepoValidator) []domain.Report {
	key := e.Key()
	if lister == nil {
		return []domain.Report{singleCheckReport(key, domain.StatusSkip,
			fmt.Sprintf("no GitHub credentials configured; cannot expand %q", key))}
	}
	repos, err := lister.ListOrgRepos(ctx, e.Org)
	if err != nil {
		return []domain.Report{singleCheckReport(key, domain.StatusFail,
			fmt.Sprintf("list repos in %s: %v", e.Org, err))}
	}
	out := make([]domain.Report, 0, len(repos))
	for _, r := range repos {
		out = append(out, v.Validate(ctx, e.Org+"/"+r))
	}
	return out
}

func singleCheckReport(repository string, status domain.Status, detail string) domain.Report {
	return domain.Report{
		Repository: repository,
		Checks:     []domain.CheckResult{{Name: "org-repos", Status: status, Detail: detail}},
	}
}
