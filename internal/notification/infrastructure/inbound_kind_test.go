package infrastructure_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/infrastructure"
)

// dispatchKind posts a webhook body through the GitHub handler and returns the
// event handed to the dispatcher. Every recognised payload still returns 200 and
// dispatches; an unmapped one dispatches KindUnknown so the dispatcher debug-logs
// no_handler.
func dispatchKind(t *testing.T, githubEvent, body string) kernel.Event {
	t.Helper()

	dispatcher := &fakeDispatcher{}
	recorder := postGitHubWebhook(infrastructure.NewGitHubHandler(dispatcher), githubEvent, body)

	require.Equal(t, http.StatusOK, recorder.Code, "a recognised body dispatches rather than erroring")
	require.True(t, dispatcher.called)
	return dispatcher.event
}

// TestMapKind pins every GitHub payload-to-kind mapping the adapter owns —
// draft gating, merged-vs-closed, the review-state split, the edited-commented
// case, line/conversation comments, plain-issue comments producing no event, and
// unmapped actions. Handlers rely on these kinds alone, so a regression here would
// silently change delivery behavior.
func TestMapKind(t *testing.T) {
	testCases := []struct {
		name        string
		githubEvent string
		body        string
		want        kernel.EventKind
	}{
		{
			name:        "opened non-draft",
			githubEvent: "pull_request",
			body:        `{"action":"opened","repository":{"full_name":"o/r"},"pull_request":{"number":7,"draft":false,"user":{"login":"a"}}}`,
			want:        kernel.KindOpened,
		},
		{
			name:        "opened draft is gated to unknown",
			githubEvent: "pull_request",
			body:        `{"action":"opened","repository":{"full_name":"o/r"},"pull_request":{"number":7,"draft":true,"user":{"login":"a"}}}`,
			want:        kernel.KindUnknown,
		},
		{
			name:        "ready_for_review",
			githubEvent: "pull_request",
			body:        `{"action":"ready_for_review","repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:        kernel.KindReadyForReview,
		},
		{
			name:        "converted_to_draft",
			githubEvent: "pull_request",
			body:        `{"action":"converted_to_draft","repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:        kernel.KindConvertedToDraft,
		},
		{
			name:        "closed not merged",
			githubEvent: "pull_request",
			body:        `{"action":"closed","repository":{"full_name":"o/r"},"pull_request":{"number":7,"merged":false,"user":{"login":"a"}}}`,
			want:        kernel.KindClosed,
		},
		{
			name:        "closed merged",
			githubEvent: "pull_request",
			body:        `{"action":"closed","repository":{"full_name":"o/r"},"pull_request":{"number":7,"merged":true,"user":{"login":"a"}}}`,
			want:        kernel.KindMerged,
		},
		{
			name:        "pull_request synchronize is unmapped",
			githubEvent: "pull_request",
			body:        `{"action":"synchronize","repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:        kernel.KindUnknown,
		},
		{
			name:        "review submitted approved",
			githubEvent: "pull_request_review",
			body:        `{"action":"submitted","review":{"state":"approved"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:        kernel.KindApproved,
		},
		{
			name:        "review submitted changes_requested",
			githubEvent: "pull_request_review",
			body:        `{"action":"submitted","review":{"state":"changes_requested"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:        kernel.KindChangesRequested,
		},
		{
			name:        "review submitted commented finishes session",
			githubEvent: "pull_request_review",
			body:        `{"action":"submitted","review":{"state":"commented"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:        kernel.KindReviewCommented,
		},
		{
			name:        "review edited commented is a plain comment",
			githubEvent: "pull_request_review",
			body:        `{"action":"edited","review":{"state":"commented"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:        kernel.KindCommented,
		},
		{
			name:        "review edited approved is unmapped",
			githubEvent: "pull_request_review",
			body:        `{"action":"edited","review":{"state":"approved"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:        kernel.KindUnknown,
		},
		{
			name:        "review submitted with no review object is unmapped",
			githubEvent: "pull_request_review",
			body:        `{"action":"submitted","repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:        kernel.KindUnknown,
		},
		{
			name:        "line comment created",
			githubEvent: "pull_request_review_comment",
			body:        `{"action":"created","repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:        kernel.KindCommented,
		},
		{
			name:        "line comment edited is unmapped",
			githubEvent: "pull_request_review_comment",
			body:        `{"action":"edited","repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:        kernel.KindUnknown,
		},
		{
			name:        "conversation comment created on a PR",
			githubEvent: "issue_comment",
			body:        `{"action":"created","repository":{"full_name":"o/r"},"issue":{"number":7,"pull_request":{"url":"u"}}}`,
			want:        kernel.KindCommented,
		},
		{
			name:        "conversation comment edited on a PR is unmapped",
			githubEvent: "issue_comment",
			body:        `{"action":"edited","repository":{"full_name":"o/r"},"issue":{"number":7,"pull_request":{"url":"u"}}}`,
			want:        kernel.KindUnknown,
		},
		{
			name:        "plain-issue comment produces no event",
			githubEvent: "issue_comment",
			body:        `{"action":"created","repository":{"full_name":"o/r"},"issue":{"number":7}}`,
			want:        kernel.KindUnknown,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			event := dispatchKind(t, testCase.githubEvent, testCase.body)

			assert.Equal(t, testCase.want, event.Kind)
			assert.Equal(t, kernel.ProviderGitHub, event.Provider)
		})
	}
}

// The adapter resolves GitHub's sender.type to the neutral Sender.IsBot — the
// signal the ignore-AI-reviews policy consults.
func TestToEvent_SenderIsBot(t *testing.T) {
	bot := dispatchKind(t, "pull_request_review",
		`{"action":"submitted","review":{"state":"approved"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}},"sender":{"login":"dependabot[bot]","type":"Bot"}}`)
	human := dispatchKind(t, "pull_request_review",
		`{"action":"submitted","review":{"state":"approved"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}},"sender":{"login":"alice","type":"User"}}`)

	assert.True(t, bot.Sender.IsBot)
	assert.Equal(t, "dependabot[bot]", bot.Sender.Login)
	assert.False(t, human.Sender.IsBot)
}
