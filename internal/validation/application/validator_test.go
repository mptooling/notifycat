package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/validation/application"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

// happy returns mocks that all report success for repo "acme/widgets" mapped to
// channel C1234567. Tests override individual fields to inject failures.
func happy() (*mockMappingLookup, *mockSlackChecker, *mockHookChecker) {
	return happyMappingLookup(), happySlack(), happyGitHub()
}

// githubProbe wraps a HookChecker in the GitHub-flavored HookProbe the tests
// exercise. Pass nil to model "no API token configured".
func githubProbe(hooks domain.HookChecker) domain.HookProbe {
	return domain.HookProbe{
		Checker:        hooks,
		URLSuffix:      domain.WebhookURLPathGitHub,
		RequiredEvents: domain.RequiredGitHubEvents,
	}
}

func happyMappingLookup() *mockMappingLookup {
	return &mockMappingLookup{
		get: func(_ context.Context, repository string) (routingdomain.RepoMapping, error) {
			if repository != "acme/widgets" {
				return routingdomain.RepoMapping{}, routingdomain.ErrNotFound
			}
			return routingdomain.RepoMapping{Repository: "acme/widgets", SlackChannel: "C1234567"}, nil
		},
	}
}

func happySlack() *mockSlackChecker {
	return &mockSlackChecker{
		authTest: func(_ context.Context) (string, []string, error) {
			return "UBOT", []string{"chat:write", "reactions:write", "channels:read"}, nil
		},
		conversationsInfo: func(_ context.Context, _ string) (domain.ChannelInfo, error) {
			return domain.ChannelInfo{ID: "C1234567", Name: "general", IsMember: true}, nil
		},
	}
}

func happyGitHub() *mockHookChecker {
	return &mockHookChecker{
		listHookEvents: func(_ context.Context, _, _, _ string) ([]string, error) {
			return []string{"pull_request", "pull_request_review", "pull_request_review_comment", "issue_comment"}, nil
		},
	}
}

// findCheck returns the CheckResult with the given name, or fails the test.
func findCheck(t *testing.T, report domain.Report, name string) domain.CheckResult {
	t.Helper()

	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	require.FailNowf(t, "missing check", "no %q check in report: %+v", name, report.Checks)
	return domain.CheckResult{}
}

// assertCheckFails asserts the named check failed and explains why in its detail.
func assertCheckFails(t *testing.T, report domain.Report, name, wantDetail string) {
	t.Helper()

	check := findCheck(t, report, name)
	assert.Equal(t, domain.StatusFail, check.Status)
	assert.Contains(t, check.Detail, wantDetail)
}

func TestValidate_AllPass(t *testing.T) {
	mappings, slack, hooks := happy()
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	assert.True(t, report.OK(), "checks: %+v", report.Checks)
}

func TestValidate_MappingNotFound(t *testing.T) {
	mappings, slack, hooks := happy()
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "ghost/repo")

	assert.False(t, report.OK())
	assertCheckFails(t, report, "mapping", "no mapping found")
}

func TestValidate_PathChannelsProbed(t *testing.T) {
	mappings, slack, hooks := happy()
	mappings.additionalChannels = func(string) []string { return []string{"C0AUTH00000"} }
	var probed []string
	slack.conversationsInfo = func(_ context.Context, channel string) (domain.ChannelInfo, error) {
		probed = append(probed, channel)
		return domain.ChannelInfo{ID: channel, Name: "ok", IsMember: true}, nil
	}
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	assert.True(t, report.OK(), "checks: %+v", report.Checks)
	assert.Equal(t, []string{"C1234567", "C0AUTH00000"}, probed, "the base and every path channel are probed")
	assert.Equal(t, domain.StatusOK, findCheck(t, report, "slack-channel C0AUTH00000").Status,
		"a path channel gets its own named check")
}

func TestValidate_ChecksEveryBaseListChannel(t *testing.T) {
	var probed []string
	mappings := &mockMappingLookup{
		get: func(_ context.Context, repository string) (routingdomain.RepoMapping, error) {
			return routingdomain.RepoMapping{Repository: repository, SlackChannel: "C0B1"}, nil
		},
		additionalChannels: func(string) []string { return []string{"C0B2"} },
	}
	slack := happySlack()
	slack.conversationsInfo = func(_ context.Context, channel string) (domain.ChannelInfo, error) {
		probed = append(probed, channel)
		return domain.ChannelInfo{IsMember: true}, nil
	}
	validator := application.NewValidator(mappings, slack, domain.HookProbe{})

	validator.Validate(context.Background(), "acme/api")

	assert.ElementsMatch(t, []string{"C0B1", "C0B2"}, probed)
}

func TestValidate_PathChannelBotNotMemberFails(t *testing.T) {
	mappings, slack, hooks := happy()
	mappings.additionalChannels = func(string) []string { return []string{"C0AUTH00000"} }
	slack.conversationsInfo = func(_ context.Context, channel string) (domain.ChannelInfo, error) {
		// The bot is in the base channel but not the path channel.
		return domain.ChannelInfo{ID: channel, Name: channel, IsMember: channel == "C1234567"}, nil
	}
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	assert.False(t, report.OK())
	assert.Equal(t, domain.StatusFail, findCheck(t, report, "slack-channel C0AUTH00000").Status)
}
