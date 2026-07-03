package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mptooling/notifycat/internal/github"
	"github.com/mptooling/notifycat/internal/maintenance/domain"
)

// PRStateGetter is the slice of the GitHub client the checker needs.
type PRStateGetter interface {
	GetPullRequest(ctx context.Context, owner, repo string, number int) (github.PullRequestState, error)
}

// GitHubChecker adapts the GitHub client to domain.PRChecker: it splits
// "owner/repo" and reports whether the PR's state is "open". A 404 maps to
// domain.ErrPRNotFound and a draft PR (any state) to domain.ErrPRDraft, so the
// reconciler can drop either from the digest; any other API error is propagated
// verbatim, so the reconciler leaves the row untouched rather than wrongly
// acting on a PR it could not read (e.g. a transient 5xx).
type GitHubChecker struct {
	gh PRStateGetter
}

// NewGitHubChecker wraps a GitHub client.
func NewGitHubChecker(gh PRStateGetter) *GitHubChecker {
	return &GitHubChecker{gh: gh}
}

// IsOpen implements domain.PRChecker: it reports whether the PR's GitHub state
// is "open" (merged and closed PRs both report "closed").
func (c *GitHubChecker) IsOpen(ctx context.Context, repository string, number int) (bool, error) {
	owner, repo, ok := strings.Cut(repository, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return false, fmt.Errorf("reconcile: invalid repository %q", repository)
	}
	st, err := c.gh.GetPullRequest(ctx, owner, repo, number)
	if err != nil {
		var apiErr *github.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return false, fmt.Errorf("%w: %w", domain.ErrPRNotFound, err)
		}
		return false, err
	}
	// A draft PR must never stay in the database, regardless of open/closed
	// state — this takes precedence over the merged/closed disposition.
	if st.Draft {
		return false, domain.ErrPRDraft
	}
	return st.State == "open", nil
}
