package infrastructure_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	"github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
)

// bitbucketForgedPayload mirrors the fields rawBitbucketPayload reads in
// internal/notification/infrastructure/inbound_bitbucket.go.
type bitbucketForgedPayload struct {
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

// buildBitbucketWebhook forges a webhook for the given smoke event and decodes its body.
func buildBitbucketWebhook(t *testing.T, repository string, prNumber int, title string, event diagnosticsdomain.SmokeEvent) (diagnosticsdomain.ForgedWebhook, bitbucketForgedPayload) {
	t.Helper()

	forged, err := infrastructure.NewBitbucketWebhookBuilder().Build(repository, prNumber, title, event)
	require.NoError(t, err)

	var payload bitbucketForgedPayload
	require.NoError(t, json.Unmarshal(forged.Body, &payload), "body = %s", forged.Body)
	return forged, payload
}

func TestBitbucketWebhookBuilder_Opened(t *testing.T) {
	forged, payload := buildBitbucketWebhook(t, "org/repo", 42, "My PR",
		diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeOpened})

	assert.Equal(t, "X-Event-Key", forged.EventHeader)
	assert.Equal(t, "pullrequest:created", forged.EventValue)
	assert.Equal(t, 42, payload.PullRequest.ID)
	assert.Equal(t, "My PR", payload.PullRequest.Title)
	assert.Equal(t, "OPEN", payload.PullRequest.State)
	assert.False(t, payload.PullRequest.Draft)
	assert.Equal(t, "user", payload.Actor.Type)
	assert.Equal(t, "notifycat-smoke", payload.Actor.DisplayName)
	assert.Equal(t, "notifycat-smoke", payload.PullRequest.Author.DisplayName)
	assert.Equal(t, "user", payload.PullRequest.Author.Type)
	assert.Equal(t, "org/repo", payload.Repository.FullName)
	assert.Equal(t, "https://bitbucket.org/org/repo/pull-requests/42", payload.PullRequest.Links.HTML.Href)
	assert.NotEmpty(t, payload.PullRequest.CreatedOn, "created_on carries an RFC3339 timestamp")
}

func TestBitbucketWebhookBuilder_CommentedHuman(t *testing.T) {
	forged, payload := buildBitbucketWebhook(t, "org/repo", 1, "title",
		diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeCommented, IsBot: false})

	assert.Equal(t, "pullrequest:comment_created", forged.EventValue)
	assert.Equal(t, "user", payload.Actor.Type)
}

func TestBitbucketWebhookBuilder_CommentedBot(t *testing.T) {
	forged, payload := buildBitbucketWebhook(t, "org/repo", 1, "title",
		diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeCommented, IsBot: true})

	assert.Equal(t, "pullrequest:comment_created", forged.EventValue)
	assert.Equal(t, "app_user", payload.Actor.Type, "a bot actor is not a plain user")
}

func TestBitbucketWebhookBuilder_Approved(t *testing.T) {
	forged, payload := buildBitbucketWebhook(t, "org/repo", 1, "title",
		diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeApproved})

	assert.Equal(t, "pullrequest:approved", forged.EventValue)
	assert.Equal(t, "OPEN", payload.PullRequest.State)
	assert.Equal(t, "user", payload.Actor.Type)
}

func TestBitbucketWebhookBuilder_Merged(t *testing.T) {
	forged, payload := buildBitbucketWebhook(t, "org/repo", 1, "title",
		diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeMerged})

	assert.Equal(t, "pullrequest:fulfilled", forged.EventValue)
	assert.Equal(t, "MERGED", payload.PullRequest.State)
	assert.Equal(t, "user", payload.Actor.Type)
}

func TestBitbucketWebhookBuilder_PRIDEqualsNumber(t *testing.T) {
	_, payload := buildBitbucketWebhook(t, "org/repo", 77, "title",
		diagnosticsdomain.SmokeEvent{Kind: diagnosticsdomain.SmokeOpened})

	assert.Equal(t, 77, payload.PullRequest.ID, "Bitbucket's id is the PR number")
}
