package reconcile

import (
	"context"
	"fmt"
	"strings"

	"github.com/mptooling/notifycat/internal/github"
)

// prGetter is the slice of *github.Client the checker needs.
type prGetter interface {
	GetPullRequest(ctx context.Context, owner, repo string, number int) (github.PullRequestState, error)
}

// GitHubChecker adapts the GitHub client to PRChecker: it splits "owner/repo"
// and reports whether the PR's state is "open". Any API error (including a 404
// for a renamed/inaccessible repo) is propagated, so the reconciler leaves the
// row untouched rather than wrongly marking closed a PR it could not read.
type GitHubChecker struct {
	gh prGetter
}

// NewGitHubChecker wraps a GitHub client.
func NewGitHubChecker(gh prGetter) *GitHubChecker {
	return &GitHubChecker{gh: gh}
}

// IsOpen reports whether the PR's GitHub state is "open" (merged and closed PRs
// both report "closed").
func (c *GitHubChecker) IsOpen(ctx context.Context, repository string, number int) (bool, error) {
	owner, repo, ok := strings.Cut(repository, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return false, fmt.Errorf("reconcile: invalid repository %q", repository)
	}
	st, err := c.gh.GetPullRequest(ctx, owner, repo, number)
	if err != nil {
		return false, err
	}
	return st.State == "open", nil
}
