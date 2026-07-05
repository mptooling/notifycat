package infrastructure

import (
	"encoding/json"
	"fmt"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
)

// GitHubWebhookBuilder implements diagnosticsdomain.WebhookBuilder by producing
// GitHub-shaped webhook bodies. Each SmokeEvent maps to the exact JSON structure
// and X-GitHub-Event header value the notification handlers expect.
type GitHubWebhookBuilder struct{}

// NewGitHubWebhookBuilder returns a GitHubWebhookBuilder.
func NewGitHubWebhookBuilder() *GitHubWebhookBuilder { return &GitHubWebhookBuilder{} }

// Build renders a ForgedWebhook for the given neutral SmokeEvent using GitHub
// webhook vocabulary. It reproduces exactly the JSON fields the GitHub inbound
// parser and notification handlers read.
func (GitHubWebhookBuilder) Build(repository string, number int, title string, ev diagnosticsdomain.SmokeEvent) (diagnosticsdomain.ForgedWebhook, error) {
	type user struct {
		Login string `json:"login"`
	}
	type review struct {
		State string `json:"state"`
	}
	payload := struct {
		Action     string `json:"action"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
			User    user   `json:"user"`
			Merged  bool   `json:"merged"`
			Draft   bool   `json:"draft"`
		} `json:"pull_request"`
		Review *review `json:"review,omitempty"`
		Sender struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"sender"`
	}{}

	payload.Repository.FullName = repository
	payload.PullRequest.Number = number
	payload.PullRequest.Title = title
	payload.PullRequest.HTMLURL = fmt.Sprintf("https://github.com/%s/pull/%d", repository, number)
	payload.PullRequest.User = user{Login: "notifycat-smoke"}
	payload.Sender.Login = "notifycat-smoke"
	payload.Sender.Type = "User"

	var eventValue string

	switch ev.Kind {
	case diagnosticsdomain.SmokeOpened:
		eventValue = "pull_request"
		payload.Action = "opened"
		// merged=false, draft=false are zero values.

	case diagnosticsdomain.SmokeCommented:
		eventValue = "pull_request_review"
		payload.Action = "submitted"
		payload.Review = &review{State: "commented"}
		if ev.IsBot {
			payload.Sender.Type = "Bot"
		}

	case diagnosticsdomain.SmokeApproved:
		eventValue = "pull_request_review"
		payload.Action = "submitted"
		payload.Review = &review{State: "approved"}

	case diagnosticsdomain.SmokeMerged:
		eventValue = "pull_request"
		payload.Action = "closed"
		payload.PullRequest.Merged = true
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return diagnosticsdomain.ForgedWebhook{}, fmt.Errorf("smoke: github builder: marshal payload: %w", err)
	}
	return diagnosticsdomain.ForgedWebhook{
		EventHeader: "X-GitHub-Event",
		EventValue:  eventValue,
		Body:        body,
	}, nil
}
