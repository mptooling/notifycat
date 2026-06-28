package validate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
	"github.com/mptooling/notifycat/internal/validate"
)

// happy returns mocks that all report success for repo "acme/widgets" mapped
// to channel C1234567. Tests override individual fields to inject failures.
func happy() (*mockMappingLookup, *mockSlackChecker, *mockGitHubChecker) {
	return happyMappingLookup(), happySlack(), happyGitHub()
}

func happyMappingLookup() *mockMappingLookup {
	return &mockMappingLookup{
		get: func(_ context.Context, repository string) (store.RepoMapping, error) {
			if repository != "acme/widgets" {
				return store.RepoMapping{}, store.ErrNotFound
			}
			return store.RepoMapping{Repository: "acme/widgets", SlackChannel: "C1234567"}, nil
		},
	}
}

func happySlack() *mockSlackChecker {
	return &mockSlackChecker{
		authTest: func(_ context.Context) (string, []string, error) {
			return "UBOT", []string{"chat:write", "reactions:write", "channels:read"}, nil
		},
		conversationsInfo: func(_ context.Context, _ string) (slack.ChannelInfo, error) {
			return slack.ChannelInfo{ID: "C1234567", Name: "general", IsMember: true}, nil
		},
	}
}

func happyGitHub() *mockGitHubChecker {
	return &mockGitHubChecker{
		listHookEvents: func(_ context.Context, _, _, _ string) ([]string, error) {
			return []string{"pull_request", "pull_request_review", "pull_request_review_comment", "issue_comment"}, nil
		},
	}
}

// findCheck returns the CheckResult with the given name, or fails the test.
func findCheck(t *testing.T, r validate.Report, name string) validate.CheckResult {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %q check in report: %+v", name, r.Checks)
	return validate.CheckResult{}
}

func TestValidate_AllPass(t *testing.T) {
	m, s, gh := happy()
	v := validate.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	if !r.OK() {
		t.Fatalf("report should be OK, got: %+v", r.Checks)
	}
}

func TestValidate_MappingNotFound(t *testing.T) {
	m, s, gh := happy()
	v := validate.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "ghost/repo")
	if r.OK() {
		t.Fatal("expected fail report for missing mapping")
	}
	c := findCheck(t, r, "mapping")
	if c.Status != validate.StatusFail || !strings.Contains(c.Detail, "no mapping found") {
		t.Fatalf("mapping check = %+v", c)
	}
}

func TestValidate_PathChannelsProbed(t *testing.T) {
	m, s, gh := happy()
	m.pathChannels = func(_ string) []string { return []string{"C0AUTH00000"} }
	var probed []string
	s.conversationsInfo = func(_ context.Context, channel string) (slack.ChannelInfo, error) {
		probed = append(probed, channel)
		return slack.ChannelInfo{ID: channel, Name: "ok", IsMember: true}, nil
	}
	v := validate.NewValidator(m, s, gh)

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
	s.conversationsInfo = func(_ context.Context, channel string) (slack.ChannelInfo, error) {
		member := channel == "C1234567" // bot is in the base channel but not the path channel
		return slack.ChannelInfo{ID: channel, Name: channel, IsMember: member}, nil
	}
	v := validate.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	if r.OK() {
		t.Fatal("expected fail: bot is not a member of the path channel")
	}
	c := findCheck(t, r, "slack-channel C0AUTH00000")
	if c.Status != validate.StatusFail {
		t.Errorf("path channel check = %+v; want FAIL", c)
	}
}
