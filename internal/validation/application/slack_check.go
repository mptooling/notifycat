package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mptooling/notifycat/internal/validation/domain"
)

// slackAuthCheck verifies the token works and folds any missing required scopes
// into the same CheckResult so operators see one clear failure line.
func (v *Validator) slackAuthCheck(ctx context.Context) domain.CheckResult {
	_, scopes, err := v.slack.AuthTest(ctx)
	if err != nil {
		return slackAuthErrorResult(err)
	}
	if missing := missingScopes(scopes, domain.RequiredSlackScopes); len(missing) > 0 {
		return domain.CheckResult{
			Name:   "slack-auth",
			Status: domain.StatusFail,
			Detail: fmt.Sprintf("Slack bot is missing scope(s) %s; reinstall the app after updating the manifest", quoteJoin(missing)),
		}
	}
	return domain.CheckResult{
		Name:   "slack-auth",
		Status: domain.StatusOK,
		Detail: fmt.Sprintf("token valid; granted scopes include %s", strings.Join(domain.RequiredSlackScopes, ", ")),
	}
}

func slackAuthErrorResult(err error) domain.CheckResult {
	var apiErr *domain.SlackAPIError
	if !errors.As(err, &apiErr) {
		return failResult("slack-auth", "auth.test transport error: %v", err)
	}
	switch apiErr.Code {
	case "invalid_auth", "token_revoked", "account_inactive", "not_authed":
		return failResult("slack-auth",
			"SLACK_BOT_TOKEN is invalid or revoked (%s); reinstall the app or rotate the token",
			apiErr.Code)
	}
	return failResult("slack-auth", "auth.test failed: %s", apiErr.Code)
}

func (v *Validator) slackChannelCheck(ctx context.Context, channel string) domain.CheckResult {
	info, err := v.slack.ConversationsInfo(ctx, channel)
	if err != nil {
		return slackChannelErrorResult(channel, err)
	}
	return interpretChannelInfo(channel, info)
}

func slackChannelErrorResult(channel string, err error) domain.CheckResult {
	var apiErr *domain.SlackAPIError
	if !errors.As(err, &apiErr) {
		return failResult("slack-channel", "conversations.info transport error: %v", err)
	}
	switch apiErr.Code {
	case "channel_not_found":
		return failResult("slack-channel", "channel %s does not exist", channel)
	case "missing_scope":
		return failResult("slack-channel",
			"conversations.info needs channels:read (or groups:read for private channels); reinstall the app with that scope")
	}
	return failResult("slack-channel", "conversations.info failed: %s", apiErr.Code)
}

func interpretChannelInfo(channel string, info domain.ChannelInfo) domain.CheckResult {
	if info.IsArchived {
		return failResult("slack-channel",
			"channel %s (#%s) is archived; unarchive it or remap to an active channel",
			channel, info.Name)
	}
	if !info.IsMember {
		return failResult("slack-channel",
			"bot is not a member of #%s; run `/invite @notifycat` in that channel",
			info.Name)
	}
	return domain.CheckResult{
		Name:   "slack-channel",
		Status: domain.StatusOK,
		Detail: fmt.Sprintf("bot is a member of #%s", info.Name),
	}
}
