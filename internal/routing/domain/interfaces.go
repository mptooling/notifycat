package domain

import "context"

// RoutingProvider resolves a repository to its behavioural mapping and, for a
// monorepo with path rules, the per-channel fan-out targets for a set of
// changed files. The application-layer provider satisfies it; the per-PR router
// depends on it.
type RoutingProvider interface {
	// Get returns the resolved mapping for "org/repo", or ErrNotFound when no
	// tier matches.
	Get(ctx context.Context, repository string) (RepoMapping, error)
	// RepoHasPathRules reports whether the repository configures a `paths:` block.
	RepoHasPathRules(repository string) bool
	// TargetsForFiles returns the fan-out destinations for a PR touching files.
	TargetsForFiles(repository string, files []string) []Target
}

// ChangedFilesReader fetches the repo-relative paths a PR touches. The GitHub
// client satisfies it.
type ChangedFilesReader interface {
	ListPullRequestFiles(ctx context.Context, owner, repo string, number int) ([]string, error)
}

// TargetResolver resolves the per-PR fan-out: the repository's behaviour plus
// the per-channel targets, layering path rules over the base tier when a
// changed-files reader is available. The result carries the changed files it
// fetched so downstream consumers reuse them.
type TargetResolver interface {
	ResolveTargets(ctx context.Context, repository string, prNumber int) (ResolvedTargets, error)
}
