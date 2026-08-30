package infrastructure_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/notification/infrastructure"
)

func parsePayload(t *testing.T, body string) infrastructure.Payload {
	t.Helper()

	payload, err := infrastructure.ParsePayload([]byte(body))
	require.NoError(t, err)
	return payload
}

func TestParsePayload_PullRequestOpened(t *testing.T) {
	payload := parsePayload(t, `{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42,
			"title": "fix",
			"html_url": "https://github.com/octo/widget/pull/42",
			"user": {"login": "alice"},
			"merged": false,
			"draft": false
		}
	}`)

	assert.Equal(t, "opened", payload.Action)
	assert.Equal(t, "octo/widget", payload.Repository)
	assert.Equal(t, 42, payload.PullRequest.Number)
	assert.Equal(t, "fix", payload.PullRequest.Title)
	assert.Equal(t, "https://github.com/octo/widget/pull/42", payload.PullRequest.URL)
	assert.Equal(t, "alice", payload.PullRequest.Author)
	assert.False(t, payload.PullRequest.Draft)
	assert.False(t, payload.PullRequest.Merged)
	assert.Nil(t, payload.Review, "a non-review event carries no review object")
}

func TestParsePayload_PullRequestBody(t *testing.T) {
	payload := parsePayload(t, `{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "bump", "html_url": "u", "user": {"login": "dependabot[bot]"},
			"body": "## Vulnerabilities fixed\n\nCVE-2026-1234."
		}
	}`)

	assert.Equal(t, "## Vulnerabilities fixed\n\nCVE-2026-1234.", payload.PullRequest.Body)
}

func TestParsePayload_CreatedAt(t *testing.T) {
	payload := parsePayload(t, `{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "fix", "html_url": "u", "user": {"login": "alice"},
			"created_at": "2026-06-05T14:04:00Z"
		}
	}`)

	assert.Equal(t, time.Date(2026, 6, 5, 14, 4, 0, 0, time.UTC), payload.PullRequest.CreatedAt.UTC())
}

func TestParsePayload_CreatedAtMalformedIsZero(t *testing.T) {
	// A missing or unparseable created_at must not fail the webhook — the
	// notifier only uses it for a cosmetic context line.
	payload := parsePayload(t, `{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "fix", "html_url": "u", "user": {"login": "alice"},
			"created_at": "not-a-time"
		}
	}`)

	assert.True(t, payload.PullRequest.CreatedAt.IsZero())
}

func TestParsePayload_ReviewApproved(t *testing.T) {
	payload := parsePayload(t, `{
		"action": "submitted",
		"review": {"state": "approved"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 7, "title": "feat", "html_url": "u", "user": {"login": "alice"}
		}
	}`)

	require.NotNil(t, payload.Review)
	assert.Equal(t, "approved", payload.Review.State)
}

func TestParsePayload_Closed_Merged(t *testing.T) {
	payload := parsePayload(t, `{
		"action": "closed",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 7, "title": "feat", "html_url": "u",
			"user": {"login": "alice"}, "merged": true
		}
	}`)

	assert.Equal(t, "closed", payload.Action)
	assert.True(t, payload.PullRequest.Merged)
}

func TestParsePayload_DraftConverted(t *testing.T) {
	payload := parsePayload(t, `{
		"action": "converted_to_draft",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 5, "title": "wip", "html_url": "u",
			"user": {"login": "alice"}
		}
	}`)

	assert.Equal(t, "converted_to_draft", payload.Action)
}

func TestParsePayload_IssueCommentOnPR(t *testing.T) {
	// issue_comment payloads carry the PR number under issue.number, and the
	// presence of issue.pull_request marks the comment as a PR conversation
	// comment rather than a plain-issue comment.
	payload := parsePayload(t, `{
		"action": "created",
		"repository": {"full_name": "octo/widget"},
		"issue": {
			"number": 42,
			"pull_request": {"url": "https://api.github.com/repos/octo/widget/pulls/42"}
		},
		"sender": {"login": "alice", "type": "User"}
	}`)

	assert.Equal(t, 42, payload.PullRequest.Number)
	assert.True(t, payload.PRComment)
}

func TestParsePayload_IssueCommentOnPlainIssue(t *testing.T) {
	// A comment on a plain issue must parse without error so the dispatcher can
	// ignore it with reason no_handler, rather than 400-ing every issue comment.
	payload := parsePayload(t, `{
		"action": "created",
		"repository": {"full_name": "octo/widget"},
		"issue": {"number": 99},
		"sender": {"login": "alice", "type": "User"}
	}`)

	assert.Zero(t, payload.PullRequest.Number)
	assert.False(t, payload.PRComment)
}

func TestParsePayload_MissingPRNumberIsError(t *testing.T) {
	_, err := infrastructure.ParsePayload([]byte(`{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {}
	}`))

	assert.ErrorIs(t, err, infrastructure.ErrMissingPRNumber)
}

func TestParsePayload_InvalidJSONIsError(t *testing.T) {
	_, err := infrastructure.ParsePayload([]byte("not-json"))

	assert.Error(t, err)
}

func TestParsePayload_SenderBot(t *testing.T) {
	payload := parsePayload(t, `{
		"action": "submitted",
		"review": {"state": "approved"},
		"sender": {"login": "copilot[bot]", "type": "Bot"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 7, "title": "feat", "html_url": "u", "user": {"login": "alice"}
		}
	}`)

	assert.Equal(t, "Bot", payload.Sender.Type)
	assert.Equal(t, "copilot[bot]", payload.Sender.Login)
}

func TestParsePayload_SenderUser(t *testing.T) {
	payload := parsePayload(t, `{
		"action": "submitted",
		"review": {"state": "approved"},
		"sender": {"login": "alice", "type": "User"},
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 7, "title": "feat", "html_url": "u", "user": {"login": "alice"}
		}
	}`)

	assert.Equal(t, "User", payload.Sender.Type)
	assert.Equal(t, "alice", payload.Sender.Login)
}

func TestParsePayload_SenderAbsentIsZeroValue(t *testing.T) {
	payload := parsePayload(t, `{
		"action": "opened",
		"repository": {"full_name": "octo/widget"},
		"pull_request": {
			"number": 42, "title": "fix", "html_url": "u", "user": {"login": "alice"}
		}
	}`)

	assert.Empty(t, payload.Sender.Type, "sender stays optional")
	assert.Empty(t, payload.Sender.Login)
}
