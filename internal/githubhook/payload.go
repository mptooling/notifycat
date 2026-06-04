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
	Event      string
	Action     string
	Repository string

	PullRequest PullRequest

	// Review is non-nil only for pull_request_review events.
	Review *Review

	// PRComment is true for issue_comment events fired on a pull request —
	// the payload carries an issue.pull_request reference. It is false for
	// comments on plain issues, which handlers ignore.
	PRComment bool

	// Sender is the actor who fired the event (GitHub's `sender` object).
	// Zero value when the field is absent in the payload.
	Sender Sender
}

// Sender identifies the actor that fired the webhook. Type is "User" for
// humans and "Bot" for GitHub Apps or legacy bot accounts.
type Sender struct {
	Login string
	Type  string
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
	// Issue is present on issue_comment events. The PR number lives under
	// issue.number, and a non-nil issue.pull_request marks the comment as a
	// pull-request conversation comment rather than a plain-issue comment.
	Issue struct {
		Number      int `json:"number"`
		PullRequest *struct {
			URL string `json:"url"`
		} `json:"pull_request"`
	} `json:"issue"`
	Sender struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"sender"`
}

// ParsePayload decodes a raw GitHub webhook body into a Payload. It validates
// only what the dispatcher needs (PR number > 0); everything else is treated
// as best-effort and surfaced to handlers, which decide their own preconditions.
func ParsePayload(body []byte) (Payload, error) {
	var raw rawPayload
	if err := json.Unmarshal(body, &raw); err != nil {
		return Payload{}, fmt.Errorf("githubhook: decode payload: %w", err)
	}
	// issue_comment events carry the PR number under issue.number; the
	// presence of issue.pull_request marks the comment as a PR conversation
	// comment. A plain-issue comment (issue.number set, no issue.pull_request)
	// parses with PRComment=false and PR number 0 so the dispatcher can ignore
	// it as no_handler rather than 400-ing every issue comment in the repo.
	number := raw.PullRequest.Number
	prComment := false
	if number == 0 && raw.Issue.PullRequest != nil {
		number = raw.Issue.Number
		prComment = true
	}
	if number == 0 && raw.Issue.Number == 0 {
		return Payload{}, ErrMissingPRNumber
	}

	p := Payload{
		Action:     raw.Action,
		Repository: raw.Repository.FullName,
		PullRequest: PullRequest{
			Number: number,
			Title:  raw.PullRequest.Title,
			URL:    raw.PullRequest.HTMLURL,
			Author: raw.PullRequest.User.Login,
			Merged: raw.PullRequest.Merged,
			Draft:  raw.PullRequest.Draft,
		},
		PRComment: prComment,
		Sender:    Sender{Login: raw.Sender.Login, Type: raw.Sender.Type},
	}
	if raw.Review != nil {
		p.Review = &Review{State: raw.Review.State}
	}
	return p, nil
}
