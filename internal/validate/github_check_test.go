package validate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/validate"
)

func TestValidate_GitHubSkippedWhenCheckerNil(t *testing.T) {
	m, s, _ := happy()
	v := validate.NewValidator(m, s, nil)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "github-webhook")
	if c.Status != validate.StatusSkip {
		t.Fatalf("github-webhook should be SKIP, got %+v", c)
	}
	if !r.OK() {
		t.Fatal("skipped checks must not count as failures")
	}
}

func TestValidate_WebhookMissingEvents(t *testing.T) {
	m, s, gh := happy()
	gh.listHookEvents = func(_ context.Context, _, _, _ string) ([]string, error) {
		return []string{"pull_request"}, nil // missing two
	}
	v := validate.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "github-webhook")
	if c.Status != validate.StatusFail {
		t.Fatalf("github-webhook = %+v", c)
	}
	if !strings.Contains(c.Detail, "pull_request_review") || !strings.Contains(c.Detail, "pull_request_review_comment") {
		t.Fatalf("detail should name both missing events, got %q", c.Detail)
	}
}

func TestValidate_NoWebhookConfigured(t *testing.T) {
	m, s, gh := happy()
	gh.listHookEvents = func(_ context.Context, _, _, _ string) ([]string, error) {
		return nil, nil
	}
	v := validate.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "github-webhook")
	if c.Status != validate.StatusFail || !strings.Contains(c.Detail, "no active webhook") {
		t.Fatalf("github-webhook = %+v", c)
	}
}
