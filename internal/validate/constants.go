package validate

import "regexp"

// WebhookURLPath is the path the GitHub webhook posts to. Used to identify
// which configured hook on a repository belongs to notifycat.
const WebhookURLPath = "/webhook/github"

// Required Slack scopes mirror what the runtime handlers actually call:
// chat.postMessage requires chat:write, reactions.add requires
// reactions:write. conversations.info itself needs channels:read or
// groups:read, but we surface that one via the slack API error code, not as
// a separate scope check.
var requiredSlackScopes = []string{"chat:write", "reactions:write"}

// requiredGitHubEvents are the webhook event types the dispatcher consumes.
var requiredGitHubEvents = []string{
	"pull_request",
	"pull_request_review",
	"pull_request_review_comment",
}

// channelIDPattern mirrors the regex enforced when `add` writes a row, but
// is re-applied here so older rows (predating the regex) still get caught.
var channelIDPattern = regexp.MustCompile(`^[CGD][A-Z0-9]{2,}$`)
