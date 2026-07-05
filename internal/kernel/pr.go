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
// events, the PR author for opened events, etc. IsBot is resolved by the inbound
// adapter from the provider's own signal (GitHub's sender.type == "Bot"); the
// per-repo ignore-AI-reviews policy consults it without knowing any provider
// vocabulary.
type Sender struct {
	Login string
	IsBot bool
}

// Event is the immutable, provider-neutral record of an incoming
// pull-request-related webhook, detached from any HTTP payload type. The inbound
// adapter builds it — mapping provider vocabulary to a Kind and resolving
// Sender.IsBot — and handlers inspect only the neutral fields to decide whether
// to run.
type Event struct {
	Provider   Provider
	Kind       EventKind
	Repository string

	PR PR

	Sender Sender
}
