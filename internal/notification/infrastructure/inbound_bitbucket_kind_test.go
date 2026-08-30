package infrastructure_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/infrastructure"
)

// dispatchBitbucketKind posts a webhook body through the Bitbucket handler and
// returns the event stamped on the dispatcher. Every payload with a valid id
// dispatches (200); an unmapped one dispatches KindUnknown so the dispatcher
// debug-logs no_handler.
func dispatchBitbucketKind(t *testing.T, eventKey, body string) kernel.Event {
	t.Helper()

	dispatcher := &fakeDispatcher{}
	recorder := postBitbucketWebhook(infrastructure.NewBitbucketHandler(dispatcher), eventKey, body)

	require.Equal(t, http.StatusOK, recorder.Code, "a body with a valid id dispatches rather than erroring")
	require.True(t, dispatcher.called)
	return dispatcher.event
}

// TestMapBitbucketKind pins every Bitbucket event-key-to-kind mapping the
// adapter owns — draft gating on create, the draft/ready split on update, the
// OPEN gate on update, merged/closed, the review kinds, and unmapped keys.
// Handlers rely on these kinds alone, so a regression here would silently change
// delivery behavior.
func TestMapBitbucketKind(t *testing.T) {
	testCases := []struct {
		name     string
		eventKey string
		body     string
		want     kernel.EventKind
	}{
		{
			name:     "created non-draft",
			eventKey: "pullrequest:created",
			body:     `{"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"draft":false,"state":"OPEN"}}`,
			want:     kernel.KindOpened,
		},
		{
			name:     "created draft is gated to unknown",
			eventKey: "pullrequest:created",
			body:     `{"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"draft":true,"state":"OPEN"}}`,
			want:     kernel.KindUnknown,
		},
		{
			name:     "updated draft is converted_to_draft",
			eventKey: "pullrequest:updated",
			body:     `{"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"draft":true,"state":"OPEN"}}`,
			want:     kernel.KindConvertedToDraft,
		},
		{
			name:     "updated ready and open is ready_for_review",
			eventKey: "pullrequest:updated",
			body:     `{"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"draft":false,"state":"OPEN"}}`,
			want:     kernel.KindReadyForReview,
		},
		{
			name:     "updated ready but non-open is unmapped",
			eventKey: "pullrequest:updated",
			body:     `{"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"draft":false,"state":"MERGED"}}`,
			want:     kernel.KindUnknown,
		},
		{
			name:     "fulfilled is merged",
			eventKey: "pullrequest:fulfilled",
			body:     `{"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"state":"MERGED"}}`,
			want:     kernel.KindMerged,
		},
		{
			name:     "rejected is closed",
			eventKey: "pullrequest:rejected",
			body:     `{"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"state":"DECLINED"}}`,
			want:     kernel.KindClosed,
		},
		{
			name:     "approved",
			eventKey: "pullrequest:approved",
			body:     `{"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"state":"OPEN"}}`,
			want:     kernel.KindApproved,
		},
		{
			name:     "changes_request_created",
			eventKey: "pullrequest:changes_request_created",
			body:     `{"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"state":"OPEN"}}`,
			want:     kernel.KindChangesRequested,
		},
		{
			name:     "comment_created",
			eventKey: "pullrequest:comment_created",
			body:     `{"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"state":"OPEN"}}`,
			want:     kernel.KindCommented,
		},
		{
			name:     "unknown event key is unmapped",
			eventKey: "pullrequest:comment_deleted",
			body:     `{"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"state":"OPEN"}}`,
			want:     kernel.KindUnknown,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			event := dispatchBitbucketKind(t, testCase.eventKey, testCase.body)

			assert.Equal(t, testCase.want, event.Kind)
			assert.Equal(t, kernel.ProviderBitbucket, event.Provider)
		})
	}
}

// The adapter resolves Bitbucket's actor.type to the neutral Sender.IsBot —
// "user" is a human, anything else (a "team" or "app_user") is a bot.
func TestToBitbucketEvent_SenderIsBot(t *testing.T) {
	testCases := []struct {
		actorType string
		wantBot   bool
	}{
		{actorType: "user", wantBot: false},
		{actorType: "team", wantBot: true},
		{actorType: "app_user", wantBot: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.actorType, func(t *testing.T) {
			body := `{"actor":{"type":"` + testCase.actorType + `","display_name":"X"},` +
				`"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"state":"OPEN"}}`

			event := dispatchBitbucketKind(t, "pullrequest:approved", body)

			assert.Equal(t, testCase.wantBot, event.Sender.IsBot)
		})
	}
}
