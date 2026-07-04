package domain

import "regexp"

// WebhookURLPath is the path the GitHub webhook posts to. Used to identify
// which configured hook on a repository belongs to notifycat.
const WebhookURLPath = "/webhook/github"

// RequiredSlackScopes mirror what the runtime handlers actually call:
// chat.postMessage requires chat:write, reactions.add requires
// reactions:write. conversations.info itself needs channels:read or
// groups:read, but we surface that one via the Slack API error code, not as a
// separate scope check.
var RequiredSlackScopes = []string{"chat:write", "reactions:write"}

// RequiredGitHubEvents are the webhook event types the dispatcher consumes.
var RequiredGitHubEvents = []string{
	"pull_request",
	"pull_request_review",
	"pull_request_review_comment",
	"issue_comment",
}

// ChannelIDPattern mirrors the regex enforced when `add` writes a row, but is
// re-applied here so older rows (predating the regex) still get caught.
var ChannelIDPattern = regexp.MustCompile(`^[CGD][A-Z0-9]{2,}$`)
