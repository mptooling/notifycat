package infrastructure

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBitbucketPayload_AllFields(t *testing.T) {
	payload, err := parseBitbucketPayload([]byte(`{
		"actor": {"type": "user", "display_name": "Jane", "nickname": "jane"},
		"pullrequest": {
			"id": 42, "title": "Fix", "description": "body text",
			"state": "OPEN", "draft": false,
			"created_on": "2026-06-05T14:04:00.000000+00:00",
			"links": {"html": {"href": "https://bitbucket.org/ws/repo/pull-requests/42"}},
			"author": {"display_name": "Bob", "type": "user"}
		},
		"repository": {"full_name": "workspace/repo"}
	}`))

	require.NoError(t, err)
	assert.Equal(t, "workspace/repo", payload.Repository)
	assert.Equal(t, 42, payload.PullRequest.ID)
	assert.Equal(t, "Fix", payload.PullRequest.Title)
	assert.Equal(t, "https://bitbucket.org/ws/repo/pull-requests/42", payload.PullRequest.URL)
	assert.Equal(t, "Bob", payload.PullRequest.Author)
	assert.Equal(t, "OPEN", payload.PullRequest.State)
	assert.False(t, payload.PullRequest.Draft)
	assert.Equal(t, "body text", payload.PullRequest.Description)
	assert.Equal(t, time.Date(2026, 6, 5, 14, 4, 0, 0, time.UTC), payload.PullRequest.CreatedAt.UTC())
	assert.Equal(t, "Jane", payload.Actor.DisplayName)
	assert.Equal(t, "user", payload.Actor.Type)
}

func TestParseBitbucketPayload_CreatedOnMalformedIsZero(t *testing.T) {
	payload, err := parseBitbucketPayload([]byte(`{
		"pullrequest": {"id": 42, "title": "x", "state": "OPEN", "created_on": "not-a-time"},
		"repository": {"full_name": "w/r"}
	}`))

	require.NoError(t, err)
	assert.True(t, payload.PullRequest.CreatedAt.IsZero())
}

func TestParseBitbucketPayload_MissingIDIsError(t *testing.T) {
	_, err := parseBitbucketPayload([]byte(`{"repository":{"full_name":"w/r"},"pullrequest":{"title":"x"}}`))

	assert.ErrorIs(t, err, ErrMissingPRNumber)
}

func TestParseBitbucketPayload_InvalidJSONIsError(t *testing.T) {
	_, err := parseBitbucketPayload([]byte("not-json"))

	assert.Error(t, err)
}

func TestToBitbucketEvent_MergedFromState(t *testing.T) {
	payload, err := parseBitbucketPayload([]byte(`{
		"pullrequest": {"id": 7, "state": "MERGED"},
		"repository": {"full_name": "w/r"}
	}`))
	require.NoError(t, err)

	event := toBitbucketEvent("pullrequest:fulfilled", payload)

	assert.True(t, event.PR.Merged, "state MERGED marks the PR merged")
}
