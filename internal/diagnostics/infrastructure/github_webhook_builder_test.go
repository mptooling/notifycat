package infrastructure_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	"github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
)

// githubForgedPayload is the union of every field the forged GitHub bodies carry.
type githubForgedPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		Merged  bool   `json:"merged"`
		Draft   bool   `json:"draft"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
	Review struct {
		State string `json:"state"`
	} `json:"review"`
	Sender struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"sender"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// buildGitHubWebhook forges a webhook for the given smoke event and decodes its body.
func buildGitHubWebhook(t *testing.T, repository string, prNumber int, title string, event diagnosticsdomain.SmokeEvent) (diagnosticsdomain.ForgedWebhook, githubForgedPayload) {
	t.Helper()

	forged, err := infrastructure.NewGitHubWebhookBuilder().Build(repository, prNumber, title, event)
	require.NoError(t, err)

	var payload githubForgedPayload
	require.NoError(t, json.Unmarshal(forged.Body, &payload), "body = %s", forged.Body)
	return forged, payload
}

func TestGitHubWebhookBuilder_Opened(t *testing.T) {
	forged, payload := buildGitHubWebhook(t, "org/repo", 42, "My PR",
		diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeOpened})

	assert.Equal(t, "X-GitHub-Event", forged.EventHeader)
	assert.Equal(t, "pull_request", forged.EventValue)
	assert.Equal(t, "opened", payload.Action)
	assert.Equal(t, 42, payload.PullRequest.Number)
	assert.Equal(t, "My PR", payload.PullRequest.Title)
	assert.Equal(t, "https://github.com/org/repo/pull/42", payload.PullRequest.HTMLURL)
	assert.False(t, payload.PullRequest.Merged)
	assert.False(t, payload.PullRequest.Draft)
	assert.Equal(t, "User", payload.Sender.Type)
	assert.Equal(t, "org/repo", payload.Repository.FullName)
}

func TestGitHubWebhookBuilder_CommentedHuman(t *testing.T) {
	forged, payload := buildGitHubWebhook(t, "org/repo", 42, "My PR",
		diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeCommented, IsBot: false})

	assert.Equal(t, "pull_request_review", forged.EventValue)
	assert.Equal(t, "submitted", payload.Action)
	assert.Equal(t, "commented", payload.Review.State)
	assert.Equal(t, "User", payload.Sender.Type)
}

func TestGitHubWebhookBuilder_CommentedBot(t *testing.T) {
	_, payload := buildGitHubWebhook(t, "org/repo", 42, "My PR",
		diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeCommented, IsBot: true})

	assert.Equal(t, "Bot", payload.Sender.Type)
}

func TestGitHubWebhookBuilder_Approved(t *testing.T) {
	forged, payload := buildGitHubWebhook(t, "org/repo", 42, "My PR",
		diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeApproved})

	assert.Equal(t, "pull_request_review", forged.EventValue)
	assert.Equal(t, "approved", payload.Review.State)
	assert.Equal(t, "User", payload.Sender.Type)
}

func TestGitHubWebhookBuilder_Merged(t *testing.T) {
	forged, payload := buildGitHubWebhook(t, "org/repo", 42, "My PR",
		diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeMerged})

	assert.Equal(t, "pull_request", forged.EventValue)
	assert.Equal(t, "closed", payload.Action)
	assert.True(t, payload.PullRequest.Merged)
	assert.Equal(t, "User", payload.Sender.Type)
}

func TestGitHubWebhookBuilder_HTMLURLFormat(t *testing.T) {
	_, payload := buildGitHubWebhook(t, "owner/name", 99, "title",
		diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeOpened})

	assert.Equal(t, "https://github.com/owner/name/pull/99", payload.PullRequest.HTMLURL)
	assert.Equal(t, "notifycat-smoke", payload.PullRequest.User.Login)
	assert.Equal(t, "notifycat-smoke", payload.Sender.Login)
}
