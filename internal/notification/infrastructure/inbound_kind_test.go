package infrastructure_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/infrastructure"
)

// dispatchKind posts a webhook body through the GitHub handler and returns the
// kind stamped on the event handed to the dispatcher. Every recognised payload
// still returns 200 and dispatches; an unmapped one dispatches KindUnknown so the
// dispatcher debug-logs no_handler.
func dispatchKind(t *testing.T, ghEvent, body string) kernel.Event {
	t.Helper()
	dispatcher := &fakeDispatcher{}
	handler := infrastructure.NewGitHubHandler(dispatcher)

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(body))
	if ghEvent != "" {
		req.Header.Set("X-GitHub-Event", ghEvent)
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

// TestMapKind pins every GitHub payload-to-kind mapping the adapter owns —
// draft gating, merged-vs-closed, the review-state split, the edited-commented
// case, line/conversation comments, plain-issue comments producing no event, and
// unmapped actions. Handlers rely on these kinds alone, so a regression here would
// silently change delivery behavior.
func TestMapKind(t *testing.T) {
	const prOpen = `"pull_request":{"number":7,"title":"t","html_url":"u","user":{"login":"a"}%s}`

	cases := []struct {
		name    string
		ghEvent string
		body    string
		want    kernel.EventKind
	}{
		{
			name:    "opened non-draft",
			ghEvent: "pull_request",
			body:    `{"action":"opened","repository":{"full_name":"o/r"},"pull_request":{"number":7,"draft":false,"user":{"login":"a"}}}`,
			want:    kernel.KindOpened,
		},
		{
			name:    "opened draft is gated to unknown",
			ghEvent: "pull_request",
			body:    `{"action":"opened","repository":{"full_name":"o/r"},"pull_request":{"number":7,"draft":true,"user":{"login":"a"}}}`,
			want:    kernel.KindUnknown,
		},
		{
			name:    "ready_for_review",
			ghEvent: "pull_request",
			body:    `{"action":"ready_for_review","repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:    kernel.KindReadyForReview,
		},
		{
			name:    "converted_to_draft",
			ghEvent: "pull_request",
			body:    `{"action":"converted_to_draft","repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:    kernel.KindConvertedToDraft,
		},
		{
			name:    "closed not merged",
			ghEvent: "pull_request",
			body:    `{"action":"closed","repository":{"full_name":"o/r"},"pull_request":{"number":7,"merged":false,"user":{"login":"a"}}}`,
			want:    kernel.KindClosed,
		},
		{
			name:    "closed merged",
			ghEvent: "pull_request",
			body:    `{"action":"closed","repository":{"full_name":"o/r"},"pull_request":{"number":7,"merged":true,"user":{"login":"a"}}}`,
			want:    kernel.KindMerged,
		},
		{
			name:    "pull_request synchronize is unmapped",
			ghEvent: "pull_request",
			body:    `{"action":"synchronize","repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:    kernel.KindUnknown,
		},
		{
			name:    "review submitted approved",
			ghEvent: "pull_request_review",
			body:    `{"action":"submitted","review":{"state":"approved"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:    kernel.KindApproved,
		},
		{
			name:    "review submitted changes_requested",
			ghEvent: "pull_request_review",
			body:    `{"action":"submitted","review":{"state":"changes_requested"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:    kernel.KindChangesRequested,
		},
		{
			name:    "review submitted commented finishes session",
			ghEvent: "pull_request_review",
			body:    `{"action":"submitted","review":{"state":"commented"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:    kernel.KindReviewCommented,
		},
		{
			name:    "review edited commented is a plain comment",
			ghEvent: "pull_request_review",
			body:    `{"action":"edited","review":{"state":"commented"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:    kernel.KindCommented,
		},
		{
			name:    "review edited approved is unmapped",
			ghEvent: "pull_request_review",
			body:    `{"action":"edited","review":{"state":"approved"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:    kernel.KindUnknown,
		},
		{
			name:    "review submitted with no review object is unmapped",
			ghEvent: "pull_request_review",
			body:    `{"action":"submitted","repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:    kernel.KindUnknown,
		},
		{
			name:    "line comment created",
			ghEvent: "pull_request_review_comment",
			body:    `{"action":"created","repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:    kernel.KindCommented,
		},
		{
			name:    "line comment edited is unmapped",
			ghEvent: "pull_request_review_comment",
			body:    `{"action":"edited","repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}}}`,
			want:    kernel.KindUnknown,
		},
		{
			name:    "conversation comment created on a PR",
			ghEvent: "issue_comment",
			body:    `{"action":"created","repository":{"full_name":"o/r"},"issue":{"number":7,"pull_request":{"url":"u"}}}`,
			want:    kernel.KindCommented,
		},
		{
			name:    "conversation comment edited on a PR is unmapped",
			ghEvent: "issue_comment",
			body:    `{"action":"edited","repository":{"full_name":"o/r"},"issue":{"number":7,"pull_request":{"url":"u"}}}`,
			want:    kernel.KindUnknown,
		},
		{
			name:    "plain-issue comment produces no event",
			ghEvent: "issue_comment",
			body:    `{"action":"created","repository":{"full_name":"o/r"},"issue":{"number":7}}`,
			want:    kernel.KindUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := dispatchKind(t, tc.ghEvent, tc.body)
			if event.Kind != tc.want {
				t.Errorf("kind = %v (%s); want %v (%s)", int(event.Kind), event.Kind, int(tc.want), tc.want)
			}
			if event.Provider != kernel.ProviderGitHub {
				t.Errorf("provider = %q; want %q", event.Provider, kernel.ProviderGitHub)
			}
		})
	}
	_ = prOpen
}

// TestToEvent_SenderIsBot pins that the adapter resolves GitHub's sender.type to
// the neutral Sender.IsBot — the signal the ignore-AI-reviews policy consults.
func TestToEvent_SenderIsBot(t *testing.T) {
	bot := dispatchKind(t, "pull_request_review",
		`{"action":"submitted","review":{"state":"approved"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}},"sender":{"login":"dependabot[bot]","type":"Bot"}}`)
	if !bot.Sender.IsBot {
		t.Error("sender.type=Bot should map to Sender.IsBot=true")
	}
	if bot.Sender.Login != "dependabot[bot]" {
		t.Errorf("sender login = %q; want dependabot[bot]", bot.Sender.Login)
	}

	human := dispatchKind(t, "pull_request_review",
		`{"action":"submitted","review":{"state":"approved"},"repository":{"full_name":"o/r"},"pull_request":{"number":7,"user":{"login":"a"}},"sender":{"login":"alice","type":"User"}}`)
	if human.Sender.IsBot {
		t.Error("sender.type=User should map to Sender.IsBot=false")
	}
}
