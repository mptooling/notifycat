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
		list: func(_ context.Context) ([]store.RepoMapping, error) {
			return []store.RepoMapping{{Repository: "acme/widgets", SlackChannel: "C1234567"}}, nil
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
			return []string{"pull_request", "pull_request_review", "pull_request_review_comment"}, nil
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

func TestValidateAll_IteratesEveryMapping(t *testing.T) {
	m, s, gh := happy()
	m.list = func(_ context.Context) ([]store.RepoMapping, error) {
		return []store.RepoMapping{
			{Repository: "acme/widgets", SlackChannel: "C1234567"},
			{Repository: "acme/other", SlackChannel: "C7654321"},
		}, nil
	}
	v := validate.NewValidator(m, s, gh)

	reports, err := v.ValidateAll(context.Background())
	if err != nil {
		t.Fatalf("ValidateAll: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("len(reports) = %d; want 2", len(reports))
	}
	if reports[0].Repository != "acme/widgets" || reports[1].Repository != "acme/other" {
		t.Fatalf("report order = [%s, %s]", reports[0].Repository, reports[1].Repository)
	}
}
