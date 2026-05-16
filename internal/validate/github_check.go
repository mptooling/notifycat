package validate

import (
	"context"
	"fmt"
	"strings"
)

func (v *Validator) githubCheck(ctx context.Context, repository string) CheckResult {
	if v.github == nil {
		return skip("github-webhook", "GITHUB_TOKEN not set; webhook coverage check skipped")
	}
	owner, repo, ok := splitRepository(repository)
	if !ok {
		return failResult("github-webhook", "repository %q is not in owner/repo form", repository)
	}
	events, err := v.github.ListHookEvents(ctx, owner, repo, WebhookURLPath)
	if err != nil {
		return failResult("github-webhook", "listing %s/%s hooks failed: %v", owner, repo, err)
	}
	return interpretHookEvents(owner, repo, events)
}

func interpretHookEvents(owner, repo string, events []string) CheckResult {
	if len(events) == 0 {
		return failResult("github-webhook",
			"no active webhook on %s/%s points at %s; create one with scripts/github-webhook-create.sh",
			owner, repo, WebhookURLPath)
	}
	if missing := missingScopes(events, requiredGitHubEvents); len(missing) > 0 {
		return failResult("github-webhook",
			"webhook on %s/%s is missing event(s) %s; edit the webhook to include them",
			owner, repo, quoteJoin(missing))
	}
	return CheckResult{
		Name:   "github-webhook",
		Status: StatusOK,
		Detail: fmt.Sprintf("webhook on %s/%s covers %s", owner, repo, strings.Join(requiredGitHubEvents, ", ")),
	}
}
