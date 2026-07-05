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

// HookChecker exposes the provider-neutral endpoint the validator needs to
// confirm a repo's webhook coverage.
//
// ListHookEvents lists the event types configured on the repo's webhooks whose
// target URL matches urlSuffix, returning the union across them or an empty
// slice when no such hook exists. Implementations should not error when no hook
// matches — "no hook" is a validation outcome, not a transport failure.
type HookChecker interface {
	ListHookEvents(ctx context.Context, owner, repo, urlSuffix string) ([]string, error)
}

// RepoLister enumerates the repositories owned by an org (GitHub) or workspace
// (Bitbucket); either fills the same owner slot. Used to expand "*" at validate
// time. May be nil; the runner reports a skip in that case.
type RepoLister interface {
	ListOrgRepos(ctx context.Context, org string) ([]string, error)
}

// HookProbe is the provider-neutral webhook-coverage check: the client that
// lists a repo's configured hook events, plus the URL suffix and required event
// set to check them against. Checker is nil when no API token is configured, in
// which case the check reports a skip.
type HookProbe struct {
	Checker        HookChecker
	URLSuffix      string
	RequiredEvents []string
}

// RepoValidator validates one repository at a time. The application Validator
// satisfies it; the entry runner depends on this narrow surface so wildcard
// expansion can be tested without standing up real Slack/GitHub clients.
type RepoValidator interface {
	Validate(ctx context.Context, repository string) Report
}
