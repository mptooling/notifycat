package pullrequest

import (
	"context"
	"log/slog"
	"strings"

	"github.com/mptooling/notifycat/internal/store"
)

// Resolver is temporarily kept as a stub while handlers are being migrated
// from Resolve to ResolveTargets. Handlers will be updated in later tasks.
type Resolver interface {
	Resolve(ctx context.Context, repository string, prNumber int) (store.RepoMapping, error)
}

// PathMappings is the slice of the mappings provider the Router consumes.
type PathMappings interface {
	Get(ctx context.Context, repository string) (store.RepoMapping, error)
	RepoHasPathRules(repository string) bool
	TargetsForFiles(repository string, files []string) []store.Target
}

// ChangedFiles fetches the repo-relative paths a PR touches.
type ChangedFiles interface {
	ListPullRequestFiles(ctx context.Context, owner, repo string, number int) ([]string, error)
}

// Router resolves routing, layering per-path rules over the base repo/org tier
// when the repository configures `paths:` and a changed-files fetcher is
// available. With no fetcher (no GitHub token) or no path rules for the repo it
// is exactly the provider's Get. A fetch error is treated softly: it logs and
// falls back to the repo tier, so a GitHub hiccup never drops a notification.
type Router struct {
	mappings PathMappings
	files    ChangedFiles // nil when no GitHub token is configured
	logger   *slog.Logger
}

// NewRouter builds a Router. files may be nil (no token) — path routing is then
// inert and every PR resolves to its repo/org tier.
func NewRouter(mappings PathMappings, files ChangedFiles, logger *slog.Logger) *Router {
	return &Router{mappings: mappings, files: files, logger: logger}
}

// ResolveTargets returns the per-repo behavior plus the fan-out targets for a
// PR. With no fetcher (no token) or no path rules it returns a single base
// target. A files-API error is soft: it logs and returns the base target.
func (r *Router) ResolveTargets(ctx context.Context, repository string, prNumber int) (store.RepoMapping, []store.Target, error) {
	behavior, err := r.mappings.Get(ctx, repository)
	if err != nil {
		return store.RepoMapping{}, nil, err
	}
	baseTarget := []store.Target{{Channel: behavior.SlackChannel, Mentions: behavior.Mentions}}

	if r.files == nil || !r.mappings.RepoHasPathRules(repository) {
		return behavior, baseTarget, nil
	}
	owner, repo, ok := splitOwnerRepo(repository)
	if !ok {
		return behavior, baseTarget, nil
	}
	files, err := r.files.ListPullRequestFiles(ctx, owner, repo, prNumber)
	if err != nil {
		r.logger.Warn("path routing: could not fetch changed files; routing to the repo tier",
			slog.String("repository", repository),
			slog.Int("pr", prNumber),
			slog.Any("err", err))
		return behavior, baseTarget, nil
	}
	return behavior, r.mappings.TargetsForFiles(repository, files), nil
}

// splitOwnerRepo splits "owner/repo" into its parts. ok is false when the input
// is not exactly one non-empty owner and one non-empty repo.
func splitOwnerRepo(repository string) (owner, repo string, ok bool) {
	i := strings.IndexByte(repository, '/')
	if i < 1 || i == len(repository)-1 {
		return "", "", false
	}
	return repository[:i], repository[i+1:], true
}
