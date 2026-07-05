package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/validation/application"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

// bitbucketProbe wraps a HookChecker in the Bitbucket-flavored HookProbe so
// the generalized hookCheck can be exercised against a non-GitHub provider.
func bitbucketProbe(hc domain.HookChecker) domain.HookProbe {
	return domain.HookProbe{
		Checker:        hc,
		URLSuffix:      domain.WebhookURLPathBitbucket,
		RequiredEvents: domain.RequiredBitbucketEvents,
	}
}

func TestValidate_WebhookSkippedWhenCheckerNil(t *testing.T) {
	m, s, _ := happy()
	v := application.NewValidator(m, s, githubProbe(nil))

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "webhook")
	if c.Status != domain.StatusSkip {
		t.Fatalf("webhook should be SKIP, got %+v", c)
	}
	if !r.OK() {
		t.Fatal("skipped checks must not count as failures")
	}
}

func TestValidate_WebhookMissingEvents(t *testing.T) {
	m, s, gh := happy()
	gh.listHookEvents = func(_ context.Context, _, _, _ string) ([]string, error) {
		return []string{"pull_request"}, nil // missing the other three
	}
	v := application.NewValidator(m, s, githubProbe(gh))

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "webhook")
	if c.Status != domain.StatusFail {
		t.Fatalf("webhook = %+v", c)
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
	v := application.NewValidator(m, s, githubProbe(gh))

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "webhook")
	if c.Status != domain.StatusFail || !strings.Contains(c.Detail, "no active webhook") {
		t.Fatalf("webhook = %+v", c)
	}
}

// TestValidate_BitbucketWebhookOK proves the generalized hookCheck accepts a
// Bitbucket HookProbe: a checker returning the full Bitbucket event set passes,
// and the URL suffix passed through is the Bitbucket path.
func TestValidate_BitbucketWebhookOK(t *testing.T) {
	m, s, _ := happy()
	var gotSuffix string
	hc := &mockHookChecker{listHookEvents: func(_ context.Context, _, _, urlSuffix string) ([]string, error) {
		gotSuffix = urlSuffix
		return append([]string(nil), domain.RequiredBitbucketEvents...), nil
	}}
	v := application.NewValidator(m, s, bitbucketProbe(hc))

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "webhook")
	if c.Status != domain.StatusOK {
		t.Fatalf("webhook should be OK, got %+v", c)
	}
	if gotSuffix != domain.WebhookURLPathBitbucket {
		t.Fatalf("checker got urlSuffix %q; want %q", gotSuffix, domain.WebhookURLPathBitbucket)
	}
}

// TestValidate_BitbucketWebhookMissingEvent proves a Bitbucket webhook missing a
// required event fails and names it.
func TestValidate_BitbucketWebhookMissingEvent(t *testing.T) {
	m, s, _ := happy()
	hc := &mockHookChecker{listHookEvents: func(_ context.Context, _, _, _ string) ([]string, error) {
		return []string{"pullrequest:created"}, nil // missing the rest
	}}
	v := application.NewValidator(m, s, bitbucketProbe(hc))

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "webhook")
	if c.Status != domain.StatusFail {
		t.Fatalf("webhook = %+v", c)
	}
	if !strings.Contains(c.Detail, "pullrequest:approved") {
		t.Fatalf("detail should name a missing event, got %q", c.Detail)
	}
}
