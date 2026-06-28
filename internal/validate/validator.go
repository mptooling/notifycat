package validate

import (
	"context"
	"errors"
	"fmt"

	"github.com/mptooling/notifycat/internal/store"
)

// Validator runs the per-mapping checks. Construct with NewValidator; the
// GitHubChecker may be nil, in which case webhook coverage is reported as
// skipped.
type Validator struct {
	mappings MappingLookup
	slack    SlackChecker
	github   GitHubChecker
}

// NewValidator builds a Validator. gh may be nil when no GitHub token is
// configured.
func NewValidator(m MappingLookup, s SlackChecker, gh GitHubChecker) *Validator {
	return &Validator{mappings: m, slack: s, github: gh}
}

// Validate runs every check for a single repository.
func (v *Validator) Validate(ctx context.Context, repository string) Report {
	m, err := v.mappings.Get(ctx, repository)
	if err != nil {
		return v.mappingLookupFailure(repository, err)
	}
	return v.validateMapping(ctx, m)
}

func (v *Validator) mappingLookupFailure(repository string, err error) Report {
	detail := fmt.Sprintf("could not load mapping for %q: %v", repository, err)
	if errors.Is(err, store.ErrNotFound) {
		detail = fmt.Sprintf("no mapping found for %q; add an entry under the org that owns it in config.yaml's mappings: section", repository)
	}
	return Report{
		Repository: repository,
		Checks:     []CheckResult{{Name: "mapping", Status: StatusFail, Detail: detail}},
	}
}

// validateMapping orchestrates every check for a single mapping row. Each
// phase is a method so this stays an at-a-glance flow.
func (v *Validator) validateMapping(ctx context.Context, m store.RepoMapping) Report {
	r := Report{Repository: m.Repository}
	r.Checks = append(r.Checks, mappingFoundCheck(m))
	format, formatOK := channelFormatCheck(m)
	r.Checks = append(r.Checks, format)
	if !formatOK {
		r.Checks = append(r.Checks,
			skip("slack-auth", "channel id is invalid"),
			skip("slack-channel", "channel id is invalid"),
			v.githubCheck(ctx, m.Repository),
		)
		return r
	}
	r.Checks = append(r.Checks, v.slackChecks(ctx, m.SlackChannel, v.mappings.PathChannels(m.Repository))...)
	r.Checks = append(r.Checks, v.githubCheck(ctx, m.Repository))
	return r
}

// slackChecks returns the auth check followed by a channel probe for the base
// channel and each per-path channel, short-circuiting every probe when auth
// itself failed. Path channels are checked so a channel the bot isn't in fails
// at validation, not at post time.
func (v *Validator) slackChecks(ctx context.Context, channel string, pathChannels []string) []CheckResult {
	auth := v.slackAuthCheck(ctx)
	if auth.Status != StatusOK {
		checks := []CheckResult{auth, skip("slack-channel", "slack auth failed; skipping channel probe")}
		for _, pc := range pathChannels {
			checks = append(checks, skip("slack-channel "+pc, "slack auth failed; skipping channel probe"))
		}
		return checks
	}
	checks := []CheckResult{auth, v.slackChannelCheck(ctx, channel)}
	for _, pc := range pathChannels {
		checks = append(checks, named("slack-channel "+pc, v.slackChannelCheck(ctx, pc)))
	}
	return checks
}

// named overrides a CheckResult's Name, used to disambiguate the per-path
// channel probes from the base "slack-channel" check in the report.
func named(name string, c CheckResult) CheckResult {
	c.Name = name
	return c
}
