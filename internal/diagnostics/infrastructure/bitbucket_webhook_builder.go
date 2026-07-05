package infrastructure

import (
	"encoding/json"
	"fmt"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
)

// BitbucketWebhookBuilder implements diagnosticsdomain.WebhookBuilder by producing
// Bitbucket-shaped webhook bodies. Each SmokeEvent maps to the JSON structure
// and X-Event-Key header value that the Bitbucket inbound parser reads.
type BitbucketWebhookBuilder struct{}

// NewBitbucketWebhookBuilder returns a BitbucketWebhookBuilder.
func NewBitbucketWebhookBuilder() *BitbucketWebhookBuilder { return &BitbucketWebhookBuilder{} }

// bbCreatedOn is a fixed valid RFC3339 timestamp used for all forged payloads.
// The inbound parser falls back to the zero time on a parse error, but a valid
// string ensures PR.CreatedAt is populated correctly.
const bbCreatedOn = "2020-01-01T00:00:00Z"

// Build renders a ForgedWebhook for the given neutral SmokeEvent using Bitbucket
// webhook vocabulary. The body's field names match rawBitbucketPayload in the
// notification inbound adapter.
func (BitbucketWebhookBuilder) Build(repository string, number int, title string, ev diagnosticsdomain.SmokeEvent) (diagnosticsdomain.ForgedWebhook, error) {
	type htmlLink struct {
		Href string `json:"href"`
	}
	type links struct {
		HTML htmlLink `json:"html"`
	}
	type author struct {
		DisplayName string `json:"display_name"`
		Type        string `json:"type"`
	}
	type actor struct {
		Type        string `json:"type"`
		DisplayName string `json:"display_name"`
	}
	type pullRequest struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		State       string `json:"state"`
		Draft       bool   `json:"draft"`
		CreatedOn   string `json:"created_on"`
		Links       links  `json:"links"`
		Author      author `json:"author"`
	}
	type repo struct {
		FullName string `json:"full_name"`
	}
	payload := struct {
		Actor       actor       `json:"actor"`
		PullRequest pullRequest `json:"pullrequest"`
		Repository  repo        `json:"repository"`
	}{
		Actor: actor{
			DisplayName: "notifycat-smoke",
			Type:        "user",
		},
		PullRequest: pullRequest{
			ID:        number,
			Title:     title,
			State:     "OPEN",
			Draft:     false,
			CreatedOn: bbCreatedOn,
			Links:     links{HTML: htmlLink{Href: fmt.Sprintf("https://bitbucket.org/%s/pull-requests/%d", repository, number)}},
			Author:    author{DisplayName: "notifycat-smoke", Type: "user"},
		},
		Repository: repo{FullName: repository},
	}

	var eventValue string

	switch ev.Kind {
	case diagnosticsdomain.SmokeOpened:
		eventValue = "pullrequest:created"
		// state OPEN, draft false, actor.type "user" — already set above.

	case diagnosticsdomain.SmokeCommented:
		eventValue = "pullrequest:comment_created"
		if ev.IsBot {
			payload.Actor.Type = "app_user"
		}

	case diagnosticsdomain.SmokeApproved:
		eventValue = "pullrequest:approved"

	case diagnosticsdomain.SmokeMerged:
		eventValue = "pullrequest:fulfilled"
		payload.PullRequest.State = "MERGED"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return diagnosticsdomain.ForgedWebhook{}, fmt.Errorf("smoke: bitbucket builder: marshal payload: %w", err)
	}
	return diagnosticsdomain.ForgedWebhook{
		EventHeader: "X-Event-Key",
		EventValue:  eventValue,
		Body:        body,
	}, nil
}
