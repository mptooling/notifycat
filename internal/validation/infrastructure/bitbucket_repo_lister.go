package infrastructure

import (
	"context"

	"github.com/mptooling/notifycat/internal/platform/bitbucket"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

// BitbucketRepoLister adapts *bitbucket.Client to the RepoLister port: Bitbucket
// lists a workspace's repositories, which fills the same "org" slot GitHub's
// org-repo listing does for wildcard expansion.
type BitbucketRepoLister struct{ client *bitbucket.Client }

// NewBitbucketRepoLister adapts a Bitbucket client to the RepoLister port.
func NewBitbucketRepoLister(client *bitbucket.Client) *BitbucketRepoLister {
	return &BitbucketRepoLister{client: client}
}

// ListOrgRepos lists the repositories in the given Bitbucket workspace,
// satisfying the RepoLister port used for wildcard expansion.
func (l *BitbucketRepoLister) ListOrgRepos(ctx context.Context, workspace string) ([]string, error) {
	return l.client.ListWorkspaceRepos(ctx, workspace)
}

var _ domain.RepoLister = (*BitbucketRepoLister)(nil)
