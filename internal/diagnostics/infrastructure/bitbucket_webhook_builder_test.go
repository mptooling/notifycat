package infrastructure_test

import (
	"encoding/json"
	"testing"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	"github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
)

// bbForgedBody mirrors the fields rawBitbucketPayload reads in
// internal/notification/infrastructure/inbound_bitbucket.go.
type bbForgedBody struct {
	Actor struct {
		Type        string `json:"type"`
		DisplayName string `json:"display_name"`
	} `json:"actor"`
	PullRequest struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		State       string `json:"state"`
		Draft       bool   `json:"draft"`
		CreatedOn   string `json:"created_on"`
		Links       struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
		Author struct {
			DisplayName string `json:"display_name"`
			Type        string `json:"type"`
		} `json:"author"`
	} `json:"pullrequest"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func decodeBBBody(t *testing.T, body []byte) bbForgedBody {
	t.Helper()
	var p bbForgedBody
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("bitbucket body is not valid JSON: %v", err)
	}
	return p
}

func TestBitbucketWebhookBuilder_Opened(t *testing.T) {
	builder := infrastructure.NewBitbucketWebhookBuilder()
	forged, err := builder.Build("org/repo", 42, "My PR", diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeOpened})
	if err != nil {
		t.Fatalf("Forge returned %v; want nil", err)
	}
	if forged.EventHeader != "X-Event-Key" {
		t.Errorf("EventHeader = %q; want X-Event-Key", forged.EventHeader)
	}
	if forged.EventValue != "pullrequest:created" {
		t.Errorf("EventValue = %q; want pullrequest:created", forged.EventValue)
	}
	p := decodeBBBody(t, forged.Body)
	if p.PullRequest.ID != 42 {
		t.Errorf("pullrequest.id = %d; want 42", p.PullRequest.ID)
	}
	if p.PullRequest.Title != "My PR" {
		t.Errorf("pullrequest.title = %q; want My PR", p.PullRequest.Title)
	}
	if p.PullRequest.State != "OPEN" {
		t.Errorf("pullrequest.state = %q; want OPEN", p.PullRequest.State)
	}
	if p.PullRequest.Draft {
		t.Error("pullrequest.draft = true; want false for opened")
	}
	if p.Actor.Type != "user" {
		t.Errorf("actor.type = %q; want user", p.Actor.Type)
	}
	if p.Actor.DisplayName != "notifycat-smoke" {
		t.Errorf("actor.display_name = %q; want notifycat-smoke", p.Actor.DisplayName)
	}
	if p.PullRequest.Author.DisplayName != "notifycat-smoke" {
		t.Errorf("author.display_name = %q; want notifycat-smoke", p.PullRequest.Author.DisplayName)
	}
	if p.PullRequest.Author.Type != "user" {
		t.Errorf("author.type = %q; want user", p.PullRequest.Author.Type)
	}
	if p.Repository.FullName != "org/repo" {
		t.Errorf("repository.full_name = %q; want org/repo", p.Repository.FullName)
	}
	if p.PullRequest.Links.HTML.Href != "https://bitbucket.org/org/repo/pull-requests/42" {
		t.Errorf("links.html.href = %q; want https://bitbucket.org/org/repo/pull-requests/42", p.PullRequest.Links.HTML.Href)
	}
	if p.PullRequest.CreatedOn == "" {
		t.Error("pullrequest.created_on is empty; want a valid RFC3339 string")
	}
}

func TestBitbucketWebhookBuilder_CommentedHuman(t *testing.T) {
	builder := infrastructure.NewBitbucketWebhookBuilder()
	forged, err := builder.Build("org/repo", 1, "title", diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeCommented, IsBot: false})
	if err != nil {
		t.Fatalf("Forge returned %v; want nil", err)
	}
	if forged.EventValue != "pullrequest:comment_created" {
		t.Errorf("EventValue = %q; want pullrequest:comment_created", forged.EventValue)
	}
	p := decodeBBBody(t, forged.Body)
	if p.Actor.Type != "user" {
		t.Errorf("actor.type = %q; want user for human comment", p.Actor.Type)
	}
}

func TestBitbucketWebhookBuilder_CommentedBot(t *testing.T) {
	builder := infrastructure.NewBitbucketWebhookBuilder()
	forged, err := builder.Build("org/repo", 1, "title", diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeCommented, IsBot: true})
	if err != nil {
		t.Fatalf("Forge returned %v; want nil", err)
	}
	if forged.EventValue != "pullrequest:comment_created" {
		t.Errorf("EventValue = %q; want pullrequest:comment_created", forged.EventValue)
	}
	p := decodeBBBody(t, forged.Body)
	if p.Actor.Type != "app_user" {
		t.Errorf("actor.type = %q; want app_user for bot comment", p.Actor.Type)
	}
}

func TestBitbucketWebhookBuilder_Approved(t *testing.T) {
	builder := infrastructure.NewBitbucketWebhookBuilder()
	forged, err := builder.Build("org/repo", 1, "title", diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeApproved})
	if err != nil {
		t.Fatalf("Forge returned %v; want nil", err)
	}
	if forged.EventValue != "pullrequest:approved" {
		t.Errorf("EventValue = %q; want pullrequest:approved", forged.EventValue)
	}
	p := decodeBBBody(t, forged.Body)
	if p.PullRequest.State != "OPEN" {
		t.Errorf("pullrequest.state = %q; want OPEN", p.PullRequest.State)
	}
	if p.Actor.Type != "user" {
		t.Errorf("actor.type = %q; want user", p.Actor.Type)
	}
}

func TestBitbucketWebhookBuilder_Merged(t *testing.T) {
	builder := infrastructure.NewBitbucketWebhookBuilder()
	forged, err := builder.Build("org/repo", 1, "title", diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeMerged})
	if err != nil {
		t.Fatalf("Forge returned %v; want nil", err)
	}
	if forged.EventValue != "pullrequest:fulfilled" {
		t.Errorf("EventValue = %q; want pullrequest:fulfilled", forged.EventValue)
	}
	p := decodeBBBody(t, forged.Body)
	if p.PullRequest.State != "MERGED" {
		t.Errorf("pullrequest.state = %q; want MERGED", p.PullRequest.State)
	}
	if p.Actor.Type != "user" {
		t.Errorf("actor.type = %q; want user", p.Actor.Type)
	}
}

func TestBitbucketWebhookBuilder_PRIDEqualsNumber(t *testing.T) {
	builder := infrastructure.NewBitbucketWebhookBuilder()
	forged, err := builder.Build("org/repo", 77, "title", diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeOpened})
	if err != nil {
		t.Fatalf("Forge returned %v; want nil", err)
	}
	p := decodeBBBody(t, forged.Body)
	if p.PullRequest.ID != 77 {
		t.Errorf("pullrequest.id = %d; want 77 (must equal number)", p.PullRequest.ID)
	}
}
