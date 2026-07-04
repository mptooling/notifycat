package infrastructure

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mptooling/notifycat/internal/platform/slack"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

// newProbe builds a SlackProbe whose client targets the given test server URL.
func newProbe(url string) *SlackProbe {
	return NewSlackProbe(slack.NewClient(http.DefaultClient, "xoxb-test", slack.WithBaseURL(url)))
}

func jsonServer(body string, header map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for k, v := range header {
			w.Header().Set(k, v)
		}
		_, _ = w.Write([]byte(body))
	}))
}

func TestSlackProbe_ConversationsInfo_MapsFields(t *testing.T) {
	srv := jsonServer(`{"ok":true,"channel":{"id":"C1","name":"general","is_member":true,"is_archived":true}}`, nil)
	defer srv.Close()

	info, err := newProbe(srv.URL).ConversationsInfo(context.Background(), "C1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := domain.ChannelInfo{ID: "C1", Name: "general", IsMember: true, IsArchived: true}
	if info != want {
		t.Fatalf("ChannelInfo = %+v; want %+v", info, want)
	}
}

func TestSlackProbe_ConversationsInfo_TranslatesAPIError(t *testing.T) {
	srv := jsonServer(`{"ok":false,"error":"channel_not_found"}`, nil)
	defer srv.Close()

	_, err := newProbe(srv.URL).ConversationsInfo(context.Background(), "C1")
	var apiErr *domain.SlackAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *domain.SlackAPIError: %v", err)
	}
	if apiErr.Code != "channel_not_found" || apiErr.Method != "conversations.info" {
		t.Fatalf("SlackAPIError = %+v; want code=channel_not_found method=conversations.info", apiErr)
	}
}

func TestSlackProbe_AuthTest_TranslatesAPIError(t *testing.T) {
	srv := jsonServer(`{"ok":false,"error":"invalid_auth"}`, nil)
	defer srv.Close()

	_, _, err := newProbe(srv.URL).AuthTest(context.Background())
	var apiErr *domain.SlackAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *domain.SlackAPIError: %v", err)
	}
	if apiErr.Code != "invalid_auth" {
		t.Fatalf("code = %q; want invalid_auth", apiErr.Code)
	}
}

func TestSlackProbe_AuthTest_ReturnsScopes(t *testing.T) {
	srv := jsonServer(`{"ok":true,"user_id":"UBOT"}`, map[string]string{"X-OAuth-Scopes": "chat:write, reactions:write"})
	defer srv.Close()

	userID, scopes, err := newProbe(srv.URL).AuthTest(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userID != "UBOT" {
		t.Fatalf("userID = %q; want UBOT", userID)
	}
	if len(scopes) != 2 || scopes[0] != "chat:write" || scopes[1] != "reactions:write" {
		t.Fatalf("scopes = %v; want [chat:write reactions:write]", scopes)
	}
}

func TestTranslateSlackError_PassThrough(t *testing.T) {
	sentinel := errors.New("dial tcp: connection refused")
	if got := translateSlackError(sentinel); !errors.Is(got, sentinel) {
		t.Fatalf("translateSlackError(transport) = %v; want pass-through %v", got, sentinel)
	}
	if got := translateSlackError(nil); got != nil {
		t.Fatalf("translateSlackError(nil) = %v; want nil", got)
	}
}
