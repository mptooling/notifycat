package domain

import (
	"context"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// MappingLookup reads a single repository → channel mapping. The runner
// iterates entries explicitly, so no bulk-list method is needed here.
// PathChannels returns the extra channels a repo's per-path routing can post
// to, so the validator can confirm bot membership in each (empty for repos
// without `paths:`).
type MappingLookup interface {
	Get(ctx context.Context, repository string) (routingdomain.RepoMapping, error)
	PathChannels(repository string) []string
}

// SlackChecker exposes the Slack endpoints the validator needs. ConversationsInfo
// returns the domain ChannelInfo, not the Slack SDK type — the infrastructure
// adapter maps across that boundary.
type SlackChecker interface {
	AuthTest(ctx context.Context) (userID string, scopes []string, err error)
	ConversationsInfo(ctx context.Context, channel string) (ChannelInfo, error)
}

// GitHubChecker exposes the GitHub endpoints the validator needs.
//
// ListHookEvents returns the union of events configured across active webhooks
// whose target URL matches urlSuffix, or an empty slice when no such hook
// exists. Implementations should not error when no hook matches — "no hook" is
// a validation outcome, not a transport failure.
type GitHubChecker interface {
	ListHookEvents(ctx context.Context, owner, repo, urlSuffix string) ([]string, error)
}

// OrgRepoLister enumerates a GitHub org's repositories. Used to expand "*" at
// validate time. May be nil; the runner reports a skip in that case.
type OrgRepoLister interface {
	ListOrgRepos(ctx context.Context, org string) ([]string, error)
}

// RepoValidator validates one repository at a time. The application Validator
// satisfies it; the entry runner depends on this narrow surface so wildcard
// expansion can be tested without standing up real Slack/GitHub clients.
type RepoValidator interface {
	Validate(ctx context.Context, repository string) Report
}
