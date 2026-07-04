package kernel

import "time"

// PR holds the pull-request fields the notification handlers and message
// composer read. It is detached from any webhook payload or persistence type.
type PR struct {
	Number int
	Title  string
	URL    string
	Author string
	Merged bool
	Draft  bool
	// Body is the PR description, consulted to tell a Dependabot/Renovate
	// security advisory from a routine bump.
	Body string
	// CreatedAt is the PR's open time, rendered as a localized date token in the
	// Slack context line. Zero when the payload omits it.
	CreatedAt time.Time
}

// Sender identifies the actor that fired a webhook — the reviewer for review
// events, the PR author for opened events, etc. Type is "User" for humans and
// "Bot" for GitHub Apps (Copilot, dependabot, …).
type Sender struct {
	Login string
	Type  string
}

// Review carries the review state of a pull_request_review event.
type Review struct {
	State ReviewState
}

// Event is the immutable record of an incoming pull-request-related webhook,
// detached from any HTTP payload type. Handlers inspect it to decide whether to
// run.
type Event struct {
	GitHubEvent GitHubEventType
	Action      Action
	Repository  string

	PR PR

	// Review is non-nil only for pull_request_review events.
	Review *Review

	// PRComment is true for issue_comment events fired on a pull request (the
	// payload carried an issue.pull_request reference). False for comments on
	// plain issues.
	PRComment bool

	Sender Sender
}
