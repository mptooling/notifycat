package reconcile

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mptooling/notifycat/internal/github"
)

// ErrPRNotFound marks a PR that GitHub reports as 404 — deleted, or in a
// repo that was renamed or is no longer accessible. The reconciler treats it
// distinctly from other API errors: rather than leave the row untouched, it
// removes the PR from the digest (a 404 will never resolve on its own).
var ErrPRNotFound = errors.New("reconcile: pull request not found")

// ErrPRDraft marks a PR that is open but in draft. The digest only nags about
// review-ready PRs, so the reconciler drops drafts the same way the live
// converted_to_draft webhook does — the difference being this catches drafts
// in the pre-tracking backlog.
var ErrPRDraft = errors.New("reconcile: pull request is a draft")

// prGetter is the slice of *github.Client the checker needs.
type prGetter interface {
	GetPullRequest(ctx context.Context, owner, repo string, number int) (github.PullRequestState, error)
}

// GitHubChecker adapts the GitHub client to PRChecker: it splits "owner/repo"
// and reports whether the PR's state is "open". A 404 is mapped to
// ErrPRNotFound and an open-but-draft PR to ErrPRDraft, so the reconciler can
// drop either from the digest; any other API error is propagated verbatim, so
// the reconciler leaves the row untouched rather than wrongly acting on a PR it
// could not read (e.g. a transient 5xx).
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
		var apiErr *github.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return false, fmt.Errorf("%w: %w", ErrPRNotFound, err)
		}
		return false, err
	}
	if st.State == "open" && st.Draft {
		return false, ErrPRDraft
	}
	return st.State == "open", nil
}
