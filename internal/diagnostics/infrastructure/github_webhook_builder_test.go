package infrastructure_test

import (
	"encoding/json"
	"testing"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	"github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
)

func TestGitHubWebhookBuilder_Opened(t *testing.T) {
	builder := infrastructure.NewGitHubWebhookBuilder()
	forged, err := builder.Build("org/repo", 42, "My PR", diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeOpened})
	if err != nil {
		t.Fatalf("Forge returned %v; want nil", err)
	}
	if forged.EventHeader != "X-GitHub-Event" {
		t.Errorf("EventHeader = %q; want X-GitHub-Event", forged.EventHeader)
	}
	if forged.EventValue != "pull_request" {
		t.Errorf("EventValue = %q; want pull_request", forged.EventValue)
	}
	var p struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
			Merged  bool   `json:"merged"`
			Draft   bool   `json:"draft"`
		} `json:"pull_request"`
		Sender struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"sender"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(forged.Body, &p); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if p.Action != "opened" {
		t.Errorf("action = %q; want opened", p.Action)
	}
	if p.PullRequest.Number != 42 {
		t.Errorf("pull_request.number = %d; want 42", p.PullRequest.Number)
	}
	if p.PullRequest.Title != "My PR" {
		t.Errorf("pull_request.title = %q; want My PR", p.PullRequest.Title)
	}
	if p.PullRequest.HTMLURL != "https://github.com/org/repo/pull/42" {
		t.Errorf("pull_request.html_url = %q; want https://github.com/org/repo/pull/42", p.PullRequest.HTMLURL)
	}
	if p.PullRequest.Merged {
		t.Error("pull_request.merged = true; want false for opened")
	}
	if p.PullRequest.Draft {
		t.Error("pull_request.draft = true; want false for opened")
	}
	if p.Sender.Type != "User" {
		t.Errorf("sender.type = %q; want User", p.Sender.Type)
	}
	if p.Repository.FullName != "org/repo" {
		t.Errorf("repository.full_name = %q; want org/repo", p.Repository.FullName)
	}
}

func TestGitHubWebhookBuilder_CommentedHuman(t *testing.T) {
	builder := infrastructure.NewGitHubWebhookBuilder()
	forged, err := builder.Build("org/repo", 42, "My PR", diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeCommented, IsBot: false})
	if err != nil {
		t.Fatalf("Forge returned %v; want nil", err)
	}
	if forged.EventValue != "pull_request_review" {
		t.Errorf("EventValue = %q; want pull_request_review", forged.EventValue)
	}
	var p struct {
		Action string `json:"action"`
		Review struct {
			State string `json:"state"`
		} `json:"review"`
		Sender struct {
			Type string `json:"type"`
		} `json:"sender"`
	}
	if err := json.Unmarshal(forged.Body, &p); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if p.Action != "submitted" {
		t.Errorf("action = %q; want submitted", p.Action)
	}
	if p.Review.State != "commented" {
		t.Errorf("review.state = %q; want commented", p.Review.State)
	}
	if p.Sender.Type != "User" {
		t.Errorf("sender.type = %q; want User", p.Sender.Type)
	}
}

func TestGitHubWebhookBuilder_CommentedBot(t *testing.T) {
	builder := infrastructure.NewGitHubWebhookBuilder()
	forged, err := builder.Build("org/repo", 42, "My PR", diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeCommented, IsBot: true})
	if err != nil {
		t.Fatalf("Forge returned %v; want nil", err)
	}
	var p struct {
		Sender struct {
			Type string `json:"type"`
		} `json:"sender"`
	}
	if err := json.Unmarshal(forged.Body, &p); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if p.Sender.Type != "Bot" {
		t.Errorf("sender.type = %q; want Bot", p.Sender.Type)
	}
}

func TestGitHubWebhookBuilder_Approved(t *testing.T) {
	builder := infrastructure.NewGitHubWebhookBuilder()
	forged, err := builder.Build("org/repo", 42, "My PR", diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeApproved})
	if err != nil {
		t.Fatalf("Forge returned %v; want nil", err)
	}
	if forged.EventValue != "pull_request_review" {
		t.Errorf("EventValue = %q; want pull_request_review", forged.EventValue)
	}
	var p struct {
		Review struct {
			State string `json:"state"`
		} `json:"review"`
		Sender struct {
			Type string `json:"type"`
		} `json:"sender"`
	}
	if err := json.Unmarshal(forged.Body, &p); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if p.Review.State != "approved" {
		t.Errorf("review.state = %q; want approved", p.Review.State)
	}
	if p.Sender.Type != "User" {
		t.Errorf("sender.type = %q; want User", p.Sender.Type)
	}
}

func TestGitHubWebhookBuilder_Merged(t *testing.T) {
	builder := infrastructure.NewGitHubWebhookBuilder()
	forged, err := builder.Build("org/repo", 42, "My PR", diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeMerged})
	if err != nil {
		t.Fatalf("Forge returned %v; want nil", err)
	}
	if forged.EventValue != "pull_request" {
		t.Errorf("EventValue = %q; want pull_request", forged.EventValue)
	}
	var p struct {
		Action      string `json:"action"`
		PullRequest struct {
			Merged bool `json:"merged"`
		} `json:"pull_request"`
		Sender struct {
			Type string `json:"type"`
		} `json:"sender"`
	}
	if err := json.Unmarshal(forged.Body, &p); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if p.Action != "closed" {
		t.Errorf("action = %q; want closed", p.Action)
	}
	if !p.PullRequest.Merged {
		t.Error("pull_request.merged = false; want true for merged")
	}
	if p.Sender.Type != "User" {
		t.Errorf("sender.type = %q; want User", p.Sender.Type)
	}
}

func TestGitHubWebhookBuilder_HTMLURLFormat(t *testing.T) {
	builder := infrastructure.NewGitHubWebhookBuilder()
	forged, err := builder.Build("owner/name", 99, "title", diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeOpened})
	if err != nil {
		t.Fatalf("Forge returned %v; want nil", err)
	}
	var p struct {
		PullRequest struct {
			HTMLURL string `json:"html_url"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"pull_request"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
	}
	if err := json.Unmarshal(forged.Body, &p); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	want := "https://github.com/owner/name/pull/99"
	if p.PullRequest.HTMLURL != want {
		t.Errorf("html_url = %q; want %q", p.PullRequest.HTMLURL, want)
	}
	if p.PullRequest.User.Login != "notifycat-smoke" {
		t.Errorf("user.login = %q; want notifycat-smoke", p.PullRequest.User.Login)
	}
	if p.Sender.Login != "notifycat-smoke" {
		t.Errorf("sender.login = %q; want notifycat-smoke", p.Sender.Login)
	}
}
