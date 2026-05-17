// Package validate verifies that a repository → Slack-channel mapping is
// usable end-to-end before GitHub fires a real PR event: the mapping row
// exists, the channel ID is well-formed, the Slack bot has the right scopes
// and is a member of the channel, and (when GitHub credentials are
// available) the webhook is subscribed to the events notifycat needs.
//
// The Validator depends only on the narrow interfaces declared below so
// tests can supply hand-written mocks without touching the real Slack or
// GitHub client packages.
package validate

import (
	"context"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// MappingLookup reads a single repository → channel mapping. The runner
// iterates entries explicitly (see RunForEntries), so no bulk-list method
// is needed here.
type MappingLookup interface {
	Get(ctx context.Context, repository string) (store.RepoMapping, error)
}

// SlackChecker exposes the Slack endpoints the validator needs.
type SlackChecker interface {
	AuthTest(ctx context.Context) (userID string, scopes []string, err error)
	ConversationsInfo(ctx context.Context, channel string) (slack.ChannelInfo, error)
}

// GitHubChecker exposes the GitHub endpoints the validator needs.
//
// ListHookEvents returns the union of events configured across active
// webhooks whose target URL matches urlSuffix, or an empty slice when no
// such hook exists. Implementations should not error when no hook matches —
// "no hook" is a validation outcome, not a transport failure.
type GitHubChecker interface {
	ListHookEvents(ctx context.Context, owner, repo, urlSuffix string) ([]string, error)
}

// Status is the outcome of a single check.
type Status int

const (
	// StatusOK means the check passed.
	StatusOK Status = iota
	// StatusFail means the check found a problem the operator must fix.
	StatusFail
	// StatusSkip means the check could not run (e.g., GitHub token absent).
	StatusSkip
)

// String renders Status as OK / FAIL / SKIP for greppable CLI output.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusFail:
		return "FAIL"
	case StatusSkip:
		return "SKIP"
	default:
		return "UNKNOWN"
	}
}

// CheckResult is one row of a Report.
type CheckResult struct {
	Name   string
	Status Status
	Detail string
}

// Report aggregates the per-check results for a single mapping.
type Report struct {
	Repository string
	Checks     []CheckResult
}

// OK returns true when no check failed. Skipped checks do not count as
// failures.
func (r Report) OK() bool {
	for _, c := range r.Checks {
		if c.Status == StatusFail {
			return false
		}
	}
	return true
}

// OrgRepoLister enumerates a GitHub org's repositories. Used to expand "*"
// at validate time. May be nil; the runner reports a skip in that case.
type OrgRepoLister interface {
	ListOrgRepos(ctx context.Context, org string) ([]string, error)
}
