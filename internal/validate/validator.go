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

// ValidateAll runs Validate for every persisted mapping, in storage order.
func (v *Validator) ValidateAll(ctx context.Context) ([]Report, error) {
	rows, err := v.mappings.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("validate: list mappings: %w", err)
	}
	reports := make([]Report, 0, len(rows))
	for _, m := range rows {
		reports = append(reports, v.validateMapping(ctx, m))
	}
	return reports, nil
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
		detail = fmt.Sprintf("no mapping found for %q; add one with `notifycat-mapping add %s <channel-id> <mentions>`", repository, repository)
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
	r.Checks = append(r.Checks, v.slackChecks(ctx, m.SlackChannel)...)
	r.Checks = append(r.Checks, v.githubCheck(ctx, m.Repository))
	return r
}

// slackChecks returns the auth + channel result pair, short-circuiting the
// channel probe when auth itself failed.
func (v *Validator) slackChecks(ctx context.Context, channel string) []CheckResult {
	auth := v.slackAuthCheck(ctx)
	if auth.Status != StatusOK {
		return []CheckResult{auth, skip("slack-channel", "slack auth failed; skipping channel probe")}
	}
	return []CheckResult{auth, v.slackChannelCheck(ctx, channel)}
}
