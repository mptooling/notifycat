// Package pullrequest holds the domain model for GitHub pull-request events
// and the handlers that update Slack in response. Adding a new event trigger
// means: write a new file in this package implementing EventHandler, register
// it in the composition root, add a unit test. The dispatcher and the rest of
// the pipeline do not change.
package pullrequest

import "context"

// Event is the immutable record of an incoming pull-request-related webhook,
// detached from any HTTP payload type. Handlers receive Event and decide via
// Applicable whether they should run.
type Event struct {
	GitHubEvent string
	Action      string
	Repository  string

	PR PR

	// Review is non-nil only for pull_request_review events.
	Review *Review
}

// PR holds the PR fields needed across handlers and the message composer.
type PR struct {
	Number int
	Title  string
	URL    string
	Author string
	Merged bool
	Draft  bool
}

// Review carries the review state (approved | commented | changes_requested).
type Review struct {
	State string
}

// EventHandler is implemented by each PR-lifecycle handler.
//
// Applicable inspects an event and returns true if Handle should run. The
// dispatcher invokes the first handler whose Applicable returns true and
// skips the rest — handlers are mutually exclusive.
type EventHandler interface {
	Applicable(Event) bool
	Handle(context.Context, Event) error
}
