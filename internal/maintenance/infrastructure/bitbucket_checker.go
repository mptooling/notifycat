package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mptooling/notifycat/internal/maintenance/domain"
	"github.com/mptooling/notifycat/internal/platform/bitbucket"
)

// BitbucketPRStateGetter is the slice of the Bitbucket client the checker needs.
type BitbucketPRStateGetter interface {
	GetPullRequest(ctx context.Context, workspace, repoSlug string, id int) (bitbucket.PullRequestState, error)
}

// BitbucketChecker adapts the Bitbucket client to domain.PRChecker: it splits
// "workspace/repo_slug" and reports whether the PR's state is "OPEN" (note:
// Bitbucket's state is uppercase). A 404 maps to domain.ErrPRNotFound and a
// draft PR (any state) to domain.ErrPRDraft, so the reconciler can drop either
// from the digest; any other API error is propagated verbatim, so the
// reconciler leaves the row untouched rather than wrongly acting on a PR it
// could not read (e.g. a transient 5xx). MERGED, DECLINED, and SUPERSEDED all
// report closed.
type BitbucketChecker struct {
	bb BitbucketPRStateGetter
}

// NewBitbucketChecker wraps a Bitbucket client.
func NewBitbucketChecker(bb BitbucketPRStateGetter) *BitbucketChecker {
	return &BitbucketChecker{bb: bb}
}

// IsOpen implements domain.PRChecker: it reports whether the PR's Bitbucket
// state is "OPEN" (MERGED, DECLINED, and SUPERSEDED all report closed).
func (c *BitbucketChecker) IsOpen(ctx context.Context, repository string, number int) (bool, error) {
	workspace, repoSlug, ok := strings.Cut(repository, "/")
	if !ok || workspace == "" || repoSlug == "" || strings.Contains(repoSlug, "/") {
		return false, fmt.Errorf("reconcile: invalid repository %q", repository)
	}
	st, err := c.bb.GetPullRequest(ctx, workspace, repoSlug, number)
	if err != nil {
		var apiErr *bitbucket.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return false, fmt.Errorf("%w: %w", domain.ErrPRNotFound, err)
		}
		return false, err
	}
	// A draft PR must never stay in the database, regardless of open/closed
	// state — this takes precedence over the merged/declined/superseded disposition.
	if st.Draft {
		return false, domain.ErrPRDraft
	}
	return st.State == "OPEN", nil
}

var _ domain.PRChecker = (*BitbucketChecker)(nil)
