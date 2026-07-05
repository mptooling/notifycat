package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/mptooling/notifycat/internal/validation/domain"
)

func (v *Validator) hookCheck(ctx context.Context, repository string) domain.CheckResult {
	if v.hook.Checker == nil {
		return skip("webhook", "no API token configured; webhook coverage check skipped")
	}
	owner, repo, ok := splitRepository(repository)
	if !ok {
		return failResult("webhook", "repository %q is not in owner/repo form", repository)
	}
	events, err := v.hook.Checker.ListHookEvents(ctx, owner, repo, v.hook.URLSuffix)
	if err != nil {
		return failResult("webhook", "listing %s/%s hooks failed: %v", owner, repo, err)
	}
	return interpretHookEvents(owner, repo, events, v.hook.RequiredEvents)
}

func interpretHookEvents(owner, repo string, events, required []string) domain.CheckResult {
	if len(events) == 0 {
		return failResult("webhook",
			"no active webhook on %s/%s points at notifycat; create one so PR events reach it",
			owner, repo)
	}
	if missing := missingScopes(events, required); len(missing) > 0 {
		return failResult("webhook",
			"webhook on %s/%s is missing event(s) %s; edit the webhook to include them",
			owner, repo, quoteJoin(missing))
	}
	return domain.CheckResult{
		Name:   "webhook",
		Status: domain.StatusOK,
		Detail: fmt.Sprintf("webhook on %s/%s covers %s", owner, repo, strings.Join(required, ", ")),
	}
}
