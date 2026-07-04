package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/mptooling/notifycat/internal/validation/domain"
)

func (v *Validator) githubCheck(ctx context.Context, repository string) domain.CheckResult {
	if v.github == nil {
		return skip("github-webhook", "GITHUB_TOKEN not set; webhook coverage check skipped")
	}
	owner, repo, ok := splitRepository(repository)
	if !ok {
		return failResult("github-webhook", "repository %q is not in owner/repo form", repository)
	}
	events, err := v.github.ListHookEvents(ctx, owner, repo, domain.WebhookURLPath)
	if err != nil {
		return failResult("github-webhook", "listing %s/%s hooks failed: %v", owner, repo, err)
	}
	return interpretHookEvents(owner, repo, events)
}

func interpretHookEvents(owner, repo string, events []string) domain.CheckResult {
	if len(events) == 0 {
		return failResult("github-webhook",
			"no active webhook on %s/%s points at %s; create one with scripts/github-webhook-create.sh",
			owner, repo, domain.WebhookURLPath)
	}
	if missing := missingScopes(events, domain.RequiredGitHubEvents); len(missing) > 0 {
		return failResult("github-webhook",
			"webhook on %s/%s is missing event(s) %s; edit the webhook to include them",
			owner, repo, quoteJoin(missing))
	}
	return domain.CheckResult{
		Name:   "github-webhook",
		Status: domain.StatusOK,
		Detail: fmt.Sprintf("webhook on %s/%s covers %s", owner, repo, strings.Join(domain.RequiredGitHubEvents, ", ")),
	}
}
