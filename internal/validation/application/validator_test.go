package application_test

import (
	"context"
	"strings"
	"testing"

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
func githubProbe(gh domain.HookChecker) domain.HookProbe {
	return domain.HookProbe{
		Checker:        gh,
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
func findCheck(t *testing.T, r domain.Report, name string) domain.CheckResult {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %q check in report: %+v", name, r.Checks)
	return domain.CheckResult{}
}

func TestValidate_AllPass(t *testing.T) {
	m, s, gh := happy()
	v := application.NewValidator(m, s, githubProbe(gh))

	r := v.Validate(context.Background(), "acme/widgets")
	if !r.OK() {
		t.Fatalf("report should be OK, got: %+v", r.Checks)
	}
}

func TestValidate_MappingNotFound(t *testing.T) {
	m, s, gh := happy()
	v := application.NewValidator(m, s, githubProbe(gh))

	r := v.Validate(context.Background(), "ghost/repo")
	if r.OK() {
		t.Fatal("expected fail report for missing mapping")
	}
	c := findCheck(t, r, "mapping")
	if c.Status != domain.StatusFail || !strings.Contains(c.Detail, "no mapping found") {
		t.Fatalf("mapping check = %+v", c)
	}
}

func TestValidate_PathChannelsProbed(t *testing.T) {
	m, s, gh := happy()
	m.pathChannels = func(_ string) []string { return []string{"C0AUTH00000"} }
	var probed []string
	s.conversationsInfo = func(_ context.Context, channel string) (domain.ChannelInfo, error) {
		probed = append(probed, channel)
		return domain.ChannelInfo{ID: channel, Name: "ok", IsMember: true}, nil
	}
	v := application.NewValidator(m, s, githubProbe(gh))

	r := v.Validate(context.Background(), "acme/widgets")
	if !r.OK() {
		t.Fatalf("report should be OK, got: %+v", r.Checks)
	}
	// Both the base and the path channel must be probed, and the path channel
	// gets its own named check.
	if len(probed) != 2 || probed[0] != "C1234567" || probed[1] != "C0AUTH00000" {
		t.Errorf("probed channels = %v; want [C1234567 C0AUTH00000]", probed)
	}
	findCheck(t, r, "slack-channel C0AUTH00000")
}

func TestValidate_PathChannelBotNotMemberFails(t *testing.T) {
	m, s, gh := happy()
	m.pathChannels = func(_ string) []string { return []string{"C0AUTH00000"} }
	s.conversationsInfo = func(_ context.Context, channel string) (domain.ChannelInfo, error) {
		member := channel == "C1234567" // bot is in the base channel but not the path channel
		return domain.ChannelInfo{ID: channel, Name: channel, IsMember: member}, nil
	}
	v := application.NewValidator(m, s, githubProbe(gh))

	r := v.Validate(context.Background(), "acme/widgets")
	if r.OK() {
		t.Fatal("expected fail: bot is not a member of the path channel")
	}
	c := findCheck(t, r, "slack-channel C0AUTH00000")
	if c.Status != domain.StatusFail {
		t.Errorf("path channel check = %+v; want FAIL", c)
	}
}
