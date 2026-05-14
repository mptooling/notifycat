package githubhook

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Payload is the parsed view of an inbound GitHub webhook body, holding only
// the fields the notifier uses. Adding a new event-type usually means
// extending this struct rather than adding a new parser.
type Payload struct {
	Action     string
	Repository string

	PullRequest PullRequest

	// Review is non-nil only for pull_request_review events.
	Review *Review
}

// PullRequest holds the PR fields extracted from the payload.
type PullRequest struct {
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

// ErrMissingPRNumber is returned when the payload lacks a pull request number.
var ErrMissingPRNumber = errors.New("githubhook: missing pull_request.number")

// rawPayload mirrors only the JSON fields we read. Unknown fields are ignored.
type rawPayload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
		Merged bool `json:"merged"`
		Draft  bool `json:"draft"`
	} `json:"pull_request"`
	Review *struct {
		State string `json:"state"`
	} `json:"review"`
}

// ParsePayload decodes a raw GitHub webhook body into a Payload. It validates
// only what the dispatcher needs (PR number > 0); everything else is treated
// as best-effort and surfaced to handlers, which decide their own preconditions.
func ParsePayload(body []byte) (Payload, error) {
	var raw rawPayload
	if err := json.Unmarshal(body, &raw); err != nil {
		return Payload{}, fmt.Errorf("githubhook: decode payload: %w", err)
	}
	if raw.PullRequest.Number == 0 {
		return Payload{}, ErrMissingPRNumber
	}

	p := Payload{
		Action:     raw.Action,
		Repository: raw.Repository.FullName,
		PullRequest: PullRequest{
			Number: raw.PullRequest.Number,
			Title:  raw.PullRequest.Title,
			URL:    raw.PullRequest.HTMLURL,
			Author: raw.PullRequest.User.Login,
			Merged: raw.PullRequest.Merged,
			Draft:  raw.PullRequest.Draft,
		},
	}
	if raw.Review != nil {
		p.Review = &Review{State: raw.Review.State}
	}
	return p, nil
}
