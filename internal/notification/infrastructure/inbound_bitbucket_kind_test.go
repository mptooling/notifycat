package infrastructure_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	handler := infrastructure.NewBitbucketHandler(dispatcher)

	req := httptest.NewRequest(http.MethodPost, "/webhook/bitbucket", strings.NewReader(body))
	if eventKey != "" {
		req.Header.Set("X-Event-Key", eventKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 (body should dispatch, not error)", rec.Code)
	}
	if !dispatcher.called {
		t.Fatal("dispatcher not called")
	}
	return dispatcher.event
}

// TestMapBitbucketKind pins every Bitbucket event-key-to-kind mapping the
// adapter owns — draft gating on create, the draft/ready split on update, the
// OPEN gate on update, merged/closed, the review kinds, and unmapped keys.
// Handlers rely on these kinds alone, so a regression here would silently change
// delivery behavior.
func TestMapBitbucketKind(t *testing.T) {
	cases := []struct {
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

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := dispatchBitbucketKind(t, tc.eventKey, tc.body)
			if event.Kind != tc.want {
				t.Errorf("kind = %v (%s); want %v (%s)", int(event.Kind), event.Kind, int(tc.want), tc.want)
			}
			if event.Provider != kernel.ProviderBitbucket {
				t.Errorf("provider = %q; want %q", event.Provider, kernel.ProviderBitbucket)
			}
		})
	}
}

// TestToBitbucketEvent_SenderIsBot pins that the adapter resolves Bitbucket's
// actor.type to the neutral Sender.IsBot — "user" is a human, anything else
// (a "team" or "app_user") is a bot.
func TestToBitbucketEvent_SenderIsBot(t *testing.T) {
	cases := []struct {
		actorType string
		wantBot   bool
	}{
		{actorType: "user", wantBot: false},
		{actorType: "team", wantBot: true},
		{actorType: "app_user", wantBot: true},
	}
	for _, tc := range cases {
		t.Run(tc.actorType, func(t *testing.T) {
			body := `{"actor":{"type":"` + tc.actorType + `","display_name":"X"},` +
				`"repository":{"full_name":"w/r"},"pullrequest":{"id":7,"state":"OPEN"}}`
			event := dispatchBitbucketKind(t, "pullrequest:approved", body)
			if event.Sender.IsBot != tc.wantBot {
				t.Errorf("actor.type=%q -> IsBot=%v; want %v", tc.actorType, event.Sender.IsBot, tc.wantBot)
			}
		})
	}
}
