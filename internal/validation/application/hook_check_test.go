package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/validation/application"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

// bitbucketProbe wraps a HookChecker in the Bitbucket-flavored HookProbe so
// the generalized hookCheck can be exercised against a non-GitHub provider.
func bitbucketProbe(hooks domain.HookChecker) domain.HookProbe {
	return domain.HookProbe{
		Checker:        hooks,
		URLSuffix:      domain.WebhookURLPathBitbucket,
		RequiredEvents: domain.RequiredBitbucketEvents,
	}
}

func TestValidate_WebhookSkippedWhenCheckerNil(t *testing.T) {
	mappings, slack, _ := happy()
	validator := application.NewValidator(mappings, slack, githubProbe(nil))

	report := validator.Validate(context.Background(), "acme/widgets")

	assert.Equal(t, domain.StatusSkip, findCheck(t, report, "webhook").Status)
	assert.True(t, report.OK(), "a skipped check is not a failure")
}

func TestValidate_WebhookMissingEvents(t *testing.T) {
	mappings, slack, hooks := happy()
	hooks.listHookEvents = func(context.Context, string, string, string) ([]string, error) {
		return []string{"pull_request"}, nil
	}
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	check := findCheck(t, report, "webhook")
	assert.Equal(t, domain.StatusWarn, check.Status)
	assert.Contains(t, check.Detail, "pull_request_review")
	assert.Contains(t, check.Detail, "pull_request_review_comment")
	assert.True(t, report.OK(), "an incomplete webhook is advisory")
}

func TestValidate_NoWebhookConfigured(t *testing.T) {
	mappings, slack, hooks := happy()
	hooks.listHookEvents = func(context.Context, string, string, string) ([]string, error) {
		return nil, nil
	}
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	check := findCheck(t, report, "webhook")
	assert.Equal(t, domain.StatusWarn, check.Status)
	assert.Contains(t, check.Detail, "no active webhook")
	assert.True(t, report.OK(), "a missing webhook is advisory")
}

// A token identity may read the repository but not list its hooks (Bitbucket
// answers 403). Coverage is then unconfirmed, which is advisory — notifycat
// itself still works.
func TestValidate_WebhookListError_Warns(t *testing.T) {
	mappings, slack, hooks := happy()
	hooks.listHookEvents = func(context.Context, string, string, string) ([]string, error) {
		return nil, errors.New("bitbucket: list-hooks: 403 Access denied. You must have write or admin access.")
	}
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	check := findCheck(t, report, "webhook")
	assert.Equal(t, domain.StatusWarn, check.Status)
	assert.Contains(t, check.Detail, "403", "the underlying error survives")
	assert.True(t, report.OK())
	assert.True(t, report.HasWarnings())
}

// The one hook outcome that stays fatal: a repository that is not owner/repo is
// a config-shape error, not external state.
func TestValidate_WebhookMalformedRepository_Fails(t *testing.T) {
	_, slack, hooks := happy()
	mappings := &mockMappingLookup{
		get: func(context.Context, string) (routingdomain.RepoMapping, error) {
			return routingdomain.RepoMapping{Repository: "acme", SlackChannel: "C1234567"}, nil
		},
	}
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme")

	assertCheckFails(t, report, "webhook", "owner/repo form")
	assert.False(t, report.OK())
}

// The generalized hookCheck accepts a Bitbucket HookProbe: a checker returning
// the full Bitbucket event set passes, and the URL suffix passed through is the
// Bitbucket path.
func TestValidate_BitbucketWebhookOK(t *testing.T) {
	mappings, slack, _ := happy()
	var gotSuffix string
	hooks := &mockHookChecker{listHookEvents: func(_ context.Context, _, _, urlSuffix string) ([]string, error) {
		gotSuffix = urlSuffix
		return append([]string(nil), domain.RequiredBitbucketEvents...), nil
	}}
	validator := application.NewValidator(mappings, slack, bitbucketProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	assert.Equal(t, domain.StatusOK, findCheck(t, report, "webhook").Status)
	assert.Equal(t, domain.WebhookURLPathBitbucket, gotSuffix)
}

func TestValidate_BitbucketWebhookMissingEvent(t *testing.T) {
	mappings, slack, _ := happy()
	hooks := &mockHookChecker{listHookEvents: func(context.Context, string, string, string) ([]string, error) {
		return []string{"pullrequest:created"}, nil
	}}
	validator := application.NewValidator(mappings, slack, bitbucketProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	check := findCheck(t, report, "webhook")
	assert.Equal(t, domain.StatusWarn, check.Status)
	assert.Contains(t, check.Detail, "pullrequest:approved")
}
