package slack_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mptooling/notifycat/internal/slack"
)

// fakeSlack is an httptest.Server that records requests against the Slack
// methods we use, and answers with canned JSON. It tracks the bearer token
// from the Authorization header so tests can verify it is sent correctly.
type fakeSlack struct {
	*httptest.Server
	mu       sync.Mutex
	calls    []recordedCall
	response func(path string, reqBody []byte, query map[string][]string) (statusCode int, respBody string)
}

type recordedCall struct {
	Method        string
	Path          string
	Body          string
	Authorization string
	ContentType   string
	Query         map[string][]string
}

func newFakeSlack(t *testing.T, response func(path string, reqBody []byte, query map[string][]string) (int, string)) *fakeSlack {
	t.Helper()
	f := &fakeSlack{response: response}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, recordedCall{
			Method:        r.Method,
			Path:          r.URL.Path,
			Body:          string(body),
			Authorization: r.Header.Get("Authorization"),
			ContentType:   r.Header.Get("Content-Type"),
			Query:         r.URL.Query(),
		})
		f.mu.Unlock()

		status, respBody := f.response(r.URL.Path, body, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeSlack) lastCall(t *testing.T) recordedCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatal("fakeSlack: no calls recorded")
	}
	return f.calls[len(f.calls)-1]
}

func TestClient_PostMessage_Success(t *testing.T) {
	fake := newFakeSlack(t, func(_ string, _ []byte, _ map[string][]string) (int, string) {
		return 200, `{"ok":true,"ts":"1700000000.0001","channel":"C123"}`
	})
	c := slack.NewClient(fake.Client(), "xoxb-test", slack.WithBaseURL(fake.URL))

	ts, err := c.PostMessage(context.Background(), "C123", "hello")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if ts != "1700000000.0001" {
		t.Fatalf("PostMessage ts = %q; want 1700000000.0001", ts)
	}

	call := fake.lastCall(t)
	if call.Method != http.MethodPost || call.Path != "/api/chat.postMessage" {
		t.Errorf("call = %s %s; want POST /api/chat.postMessage", call.Method, call.Path)
	}
	if call.Authorization != "Bearer xoxb-test" {
		t.Errorf("Authorization = %q", call.Authorization)
	}
	if !strings.Contains(call.ContentType, "application/json") {
		t.Errorf("Content-Type = %q", call.ContentType)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(call.Body), &payload); err != nil {
		t.Fatalf("body json: %v (body=%q)", err, call.Body)
	}
	if payload["channel"] != "C123" || payload["text"] != "hello" {
		t.Errorf("body payload = %v", payload)
	}
}

func TestClient_PostMessage_SlackError(t *testing.T) {
	fake := newFakeSlack(t, func(_ string, _ []byte, _ map[string][]string) (int, string) {
		return 200, `{"ok":false,"error":"channel_not_found"}`
	})
	c := slack.NewClient(fake.Client(), "xoxb-test", slack.WithBaseURL(fake.URL))

	_, err := c.PostMessage(context.Background(), "Cbad", "hi")
	if err == nil {
		t.Fatal("PostMessage with Slack error returned nil; want APIError")
	}
	var apiErr *slack.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("PostMessage err = %v; want *APIError", err)
	}
	if apiErr.Code != "channel_not_found" {
		t.Errorf("APIError.Code = %q", apiErr.Code)
	}
}

func TestClient_UpdateMessage(t *testing.T) {
	fake := newFakeSlack(t, func(_ string, _ []byte, _ map[string][]string) (int, string) {
		return 200, `{"ok":true,"ts":"1700000000.0001"}`
	})
	c := slack.NewClient(fake.Client(), "xoxb-test", slack.WithBaseURL(fake.URL))

	if err := c.UpdateMessage(context.Background(), "C1", "ts1", "edited"); err != nil {
		t.Fatalf("UpdateMessage: %v", err)
	}
	call := fake.lastCall(t)
	if call.Path != "/api/chat.update" {
		t.Errorf("path = %q", call.Path)
	}
}

func TestClient_DeleteMessage(t *testing.T) {
	fake := newFakeSlack(t, func(_ string, _ []byte, _ map[string][]string) (int, string) {
		return 200, `{"ok":true}`
	})
	c := slack.NewClient(fake.Client(), "xoxb-test", slack.WithBaseURL(fake.URL))

	if err := c.DeleteMessage(context.Background(), "C1", "ts1"); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	call := fake.lastCall(t)
	if call.Path != "/api/chat.delete" {
		t.Errorf("path = %q", call.Path)
	}
}

func TestClient_AddReaction(t *testing.T) {
	fake := newFakeSlack(t, func(_ string, _ []byte, _ map[string][]string) (int, string) {
		return 200, `{"ok":true}`
	})
	c := slack.NewClient(fake.Client(), "xoxb-test", slack.WithBaseURL(fake.URL))

	if err := c.AddReaction(context.Background(), "C1", "ts1", "rocket"); err != nil {
		t.Fatalf("AddReaction: %v", err)
	}
	call := fake.lastCall(t)
	if call.Path != "/api/reactions.add" {
		t.Errorf("path = %q", call.Path)
	}
	var payload map[string]string
	_ = json.Unmarshal([]byte(call.Body), &payload)
	if payload["channel"] != "C1" || payload["timestamp"] != "ts1" || payload["name"] != "rocket" {
		t.Errorf("payload = %v", payload)
	}
}

func TestClient_AddReaction_AlreadyReactedIsNotError(t *testing.T) {
	// Slack returns ok:false, error:"already_reacted" if the bot already
	// added that reaction. We treat it as a non-error (idempotent).
	fake := newFakeSlack(t, func(_ string, _ []byte, _ map[string][]string) (int, string) {
		return 200, `{"ok":false,"error":"already_reacted"}`
	})
	c := slack.NewClient(fake.Client(), "xoxb-test", slack.WithBaseURL(fake.URL))

	if err := c.AddReaction(context.Background(), "C1", "ts1", "rocket"); err != nil {
		t.Fatalf("AddReaction(already_reacted) returned %v; want nil", err)
	}
}

func TestClient_GetReactions(t *testing.T) {
	fake := newFakeSlack(t, func(_ string, _ []byte, _ map[string][]string) (int, string) {
		return 200, `{"ok":true,"message":{"reactions":[{"name":"rocket","users":["U1","U2"],"count":2}]}}`
	})
	c := slack.NewClient(fake.Client(), "xoxb-test", slack.WithBaseURL(fake.URL))

	reactions, err := c.GetReactions(context.Background(), "C1", "ts1")
	if err != nil {
		t.Fatalf("GetReactions: %v", err)
	}
	if len(reactions) != 1 || reactions[0].Name != "rocket" || len(reactions[0].Users) != 2 {
		t.Fatalf("reactions = %+v", reactions)
	}

	call := fake.lastCall(t)
	if call.Method != http.MethodGet || call.Path != "/api/reactions.get" {
		t.Errorf("call = %s %s; want GET /api/reactions.get", call.Method, call.Path)
	}
	if call.Query["channel"][0] != "C1" || call.Query["timestamp"][0] != "ts1" {
		t.Errorf("query = %v", call.Query)
	}
}

func TestClient_GetReactions_NoReactionsField(t *testing.T) {
	fake := newFakeSlack(t, func(_ string, _ []byte, _ map[string][]string) (int, string) {
		return 200, `{"ok":true,"message":{}}`
	})
	c := slack.NewClient(fake.Client(), "xoxb-test", slack.WithBaseURL(fake.URL))

	got, err := c.GetReactions(context.Background(), "C1", "ts1")
	if err != nil {
		t.Fatalf("GetReactions: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("reactions = %v; want empty", got)
	}
}

func TestClient_AuthTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-OAuth-Scopes", "chat:write, reactions:write,channels:read")
		_, _ = io.WriteString(w, `{"ok":true,"user_id":"UBOT123","team":"T1"}`)
	}))
	defer srv.Close()
	c := slack.NewClient(srv.Client(), "xoxb-test", slack.WithBaseURL(srv.URL))

	id, scopes, err := c.AuthTest(context.Background())
	if err != nil {
		t.Fatalf("AuthTest: %v", err)
	}
	if id != "UBOT123" {
		t.Fatalf("AuthTest user_id = %q; want UBOT123", id)
	}
	want := []string{"chat:write", "reactions:write", "channels:read"}
	if len(scopes) != len(want) {
		t.Fatalf("scopes = %v; want %v", scopes, want)
	}
	for i, s := range want {
		if scopes[i] != s {
			t.Fatalf("scopes[%d] = %q; want %q", i, scopes[i], s)
		}
	}
}

func TestClient_ConversationsInfo(t *testing.T) {
	fake := newFakeSlack(t, func(_ string, _ []byte, q map[string][]string) (int, string) {
		if got := q["channel"]; len(got) != 1 || got[0] != "C123" {
			t.Errorf("channel query = %v; want [C123]", got)
		}
		return 200, `{"ok":true,"channel":{"id":"C123","name":"general","is_member":true,"is_archived":false}}`
	})
	c := slack.NewClient(fake.Client(), "xoxb-test", slack.WithBaseURL(fake.URL))

	info, err := c.ConversationsInfo(context.Background(), "C123")
	if err != nil {
		t.Fatalf("ConversationsInfo: %v", err)
	}
	if info.ID != "C123" || info.Name != "general" || !info.IsMember || info.IsArchived {
		t.Fatalf("info = %+v", info)
	}
}

func TestClient_ConversationsInfo_NotFound(t *testing.T) {
	fake := newFakeSlack(t, func(_ string, _ []byte, _ map[string][]string) (int, string) {
		return 200, `{"ok":false,"error":"channel_not_found"}`
	})
	c := slack.NewClient(fake.Client(), "xoxb-test", slack.WithBaseURL(fake.URL))

	_, err := c.ConversationsInfo(context.Background(), "C999")
	var apiErr *slack.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "channel_not_found" {
		t.Fatalf("err = %v; want channel_not_found APIError", err)
	}
}

func TestClient_NetworkError(t *testing.T) {
	// Server that closes the connection immediately.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	defer srv.Close()
	c := slack.NewClient(srv.Client(), "xoxb-test", slack.WithBaseURL(srv.URL))

	_, err := c.PostMessage(context.Background(), "C1", "x")
	if err == nil {
		t.Fatal("PostMessage on broken server returned nil; want network error")
	}
}
