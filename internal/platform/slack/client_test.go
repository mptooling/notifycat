package slack_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/slack"
)

// fakeSlack is an httptest.Server that records requests against the Slack
// methods we use and answers with canned JSON.
type fakeSlack struct {
	*httptest.Server
	mu    sync.Mutex
	calls []recordedCall
	// retryAfter, when set, is sent as the Retry-After header on every response
	// so a rate-limit test does not have to wait out a real interval.
	retryAfter string
	response   func(path string, requestBody []byte, query map[string][]string) (statusCode int, responseBody string)
}

type recordedCall struct {
	Method        string
	Path          string
	Body          string
	Authorization string
	ContentType   string
	Query         map[string][]string
}

func newFakeSlack(t *testing.T, response func(path string, requestBody []byte, query map[string][]string) (int, string)) *fakeSlack {
	t.Helper()

	fake := &fakeSlack{response: response}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fake.mu.Lock()
		fake.calls = append(fake.calls, recordedCall{
			Method:        r.Method,
			Path:          r.URL.Path,
			Body:          string(body),
			Authorization: r.Header.Get("Authorization"),
			ContentType:   r.Header.Get("Content-Type"),
			Query:         r.URL.Query(),
		})
		fake.mu.Unlock()

		status, responseBody := fake.response(r.URL.Path, body, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		if fake.retryAfter != "" {
			w.Header().Set("Retry-After", fake.retryAfter)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, responseBody)
	}))
	t.Cleanup(fake.Close)
	return fake
}

// okJSON answers every call with the same canned success body.
func okJSON(body string) func(string, []byte, map[string][]string) (int, string) {
	return func(string, []byte, map[string][]string) (int, string) {
		return http.StatusOK, body
	}
}

func (f *fakeSlack) client() *slack.Client {
	return slack.NewClient(f.Client(), "xoxb-test", slack.WithBaseURL(f.URL))
}

func (f *fakeSlack) lastCall(t *testing.T) recordedCall {
	t.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()
	require.NotEmpty(t, f.calls, "no calls recorded")
	return f.calls[len(f.calls)-1]
}

// decodedBody unmarshals a recorded request body into a generic map.
func decodedBody(t *testing.T, call recordedCall) map[string]any {
	t.Helper()

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(call.Body), &payload), "body = %s", call.Body)
	return payload
}

// mustJSON re-encodes a decoded payload field so it can be compared with JSONEq.
func mustJSON(t *testing.T, value any) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}

func TestClient_PostMessage_Success(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":true,"ts":"1700000000.0001","channel":"C123"}`))
	message := slack.Message{
		Blocks:   []slack.Block{{Type: "section", Text: &slack.TextObject{Type: "mrkdwn", Text: "hello"}}},
		Fallback: "hello",
	}

	timestamp, err := fake.client().PostMessage(context.Background(), "C123", message)

	require.NoError(t, err)
	assert.Equal(t, "1700000000.0001", timestamp)

	call := fake.lastCall(t)
	assert.Equal(t, http.MethodPost, call.Method)
	assert.Equal(t, "/api/chat.postMessage", call.Path)
	assert.Equal(t, "Bearer xoxb-test", call.Authorization)
	assert.Contains(t, call.ContentType, "application/json")

	payload := decodedBody(t, call)
	assert.Equal(t, "C123", payload["channel"])
	assert.Equal(t, "hello", payload["text"], "the fallback becomes the notification text")
	assert.NotEmpty(t, payload["blocks"])
}

func TestClient_PostReply_ThreadsOnParent(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":true,"ts":"1700000000.0002"}`))
	message := slack.Message{
		Blocks:   []slack.Block{{Type: "section", Text: &slack.TextObject{Type: "mrkdwn", Text: "list"}}},
		Fallback: "list",
	}

	timestamp, err := fake.client().PostReply(context.Background(), "C123", "1700000000.0001", message)

	require.NoError(t, err)
	assert.Equal(t, "1700000000.0002", timestamp)

	call := fake.lastCall(t)
	assert.Equal(t, http.MethodPost, call.Method)
	assert.Equal(t, "/api/chat.postMessage", call.Path)

	payload := decodedBody(t, call)
	assert.Equal(t, "1700000000.0001", payload["thread_ts"])
	assert.Equal(t, "C123", payload["channel"])
}

func TestClient_PostMessage_SlackError(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":false,"error":"channel_not_found"}`))

	_, err := fake.client().PostMessage(context.Background(), "Cbad", slack.Message{Fallback: "hi"})

	var apiErr *slack.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "channel_not_found", apiErr.Code)
}

func TestClient_UpdateMessage(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":true,"ts":"1700000000.0001"}`))

	err := fake.client().UpdateMessage(context.Background(), "C1", "ts1", slack.Message{Fallback: "edited"})

	require.NoError(t, err)
	assert.Equal(t, "/api/chat.update", fake.lastCall(t).Path)
}

func TestClient_UpdateMessageRawBlocks(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":true,"ts":"1700000000.0001"}`))
	blocks := []json.RawMessage{
		json.RawMessage(`{"type":"section","text":{"type":"mrkdwn","text":"hello"}}`),
		json.RawMessage(`{"type":"context","elements":[{"type":"mrkdwn","text":"reviewing"}]}`),
	}

	err := fake.client().UpdateMessageRawBlocks(context.Background(), "C1", "ts1", blocks, "fallback text")

	require.NoError(t, err)
	call := fake.lastCall(t)
	assert.Equal(t, "/api/chat.update", call.Path)
	assert.Contains(t, call.Body, "reviewing", "raw blocks are forwarded verbatim")

	payload := decodedBody(t, call)
	assert.Equal(t, "C1", payload["channel"])
	assert.Equal(t, "ts1", payload["ts"])
	assert.Equal(t, "fallback text", payload["text"])
	assert.Len(t, payload["blocks"], 2)
}

func TestClient_DeleteMessage(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":true}`))

	err := fake.client().DeleteMessage(context.Background(), "C1", "ts1")

	require.NoError(t, err)
	assert.Equal(t, "/api/chat.delete", fake.lastCall(t).Path)
}

func TestClient_AddReaction(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":true}`))

	err := fake.client().AddReaction(context.Background(), "C1", "ts1", "rocket")

	require.NoError(t, err)
	call := fake.lastCall(t)
	assert.Equal(t, "/api/reactions.add", call.Path)

	payload := decodedBody(t, call)
	assert.Equal(t, "C1", payload["channel"])
	assert.Equal(t, "ts1", payload["timestamp"])
	assert.Equal(t, "rocket", payload["name"])
}

func TestClient_AddReaction_AlreadyReactedIsNotError(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":false,"error":"already_reacted"}`))

	err := fake.client().AddReaction(context.Background(), "C1", "ts1", "rocket")

	assert.NoError(t, err, "re-adding the same reaction is idempotent, not a failure")
}

func TestClient_GetReactions(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":true,"message":{"reactions":[{"name":"rocket","users":["U1","U2"],"count":2}]}}`))

	reactions, err := fake.client().GetReactions(context.Background(), "C1", "ts1")

	require.NoError(t, err)
	require.Len(t, reactions, 1)
	assert.Equal(t, "rocket", reactions[0].Name)
	assert.Equal(t, []string{"U1", "U2"}, reactions[0].Users)

	call := fake.lastCall(t)
	assert.Equal(t, http.MethodGet, call.Method)
	assert.Equal(t, "/api/reactions.get", call.Path)
	assert.Equal(t, []string{"C1"}, call.Query["channel"])
	assert.Equal(t, []string{"ts1"}, call.Query["timestamp"])
}

func TestClient_GetReactions_NoReactionsField(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":true,"message":{}}`))

	got, err := fake.client().GetReactions(context.Background(), "C1", "ts1")

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestClient_AuthTest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-OAuth-Scopes", "chat:write, reactions:write,channels:read")
		_, _ = io.WriteString(w, `{"ok":true,"user_id":"UBOT123","team":"T1"}`)
	}))
	defer server.Close()
	client := slack.NewClient(server.Client(), "xoxb-test", slack.WithBaseURL(server.URL))

	botUserID, scopes, err := client.AuthTest(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "UBOT123", botUserID)
	assert.Equal(t, []string{"chat:write", "reactions:write", "channels:read"}, scopes, "scopes are split and trimmed")
}

func TestClient_ConversationsInfo(t *testing.T) {
	fake := newFakeSlack(t, func(_ string, _ []byte, query map[string][]string) (int, string) {
		assert.Equal(t, []string{"C123"}, query["channel"])
		return http.StatusOK, `{"ok":true,"channel":{"id":"C123","name":"general","is_member":true,"is_archived":false}}`
	})

	info, err := fake.client().ConversationsInfo(context.Background(), "C123")

	require.NoError(t, err)
	assert.Equal(t, "C123", info.ID)
	assert.Equal(t, "general", info.Name)
	assert.True(t, info.IsMember)
	assert.False(t, info.IsArchived)
}

func TestClient_ConversationsInfo_NotFound(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":false,"error":"channel_not_found"}`))

	_, err := fake.client().ConversationsInfo(context.Background(), "C999")

	var apiErr *slack.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "channel_not_found", apiErr.Code)
}

func TestClient_NetworkError(t *testing.T) {
	// A server that closes the connection without answering.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker, _ := w.(http.Hijacker)
		conn, _, _ := hijacker.Hijack()
		_ = conn.Close()
	}))
	defer server.Close()
	client := slack.NewClient(server.Client(), "xoxb-test", slack.WithBaseURL(server.URL))

	_, err := client.PostMessage(context.Background(), "C1", slack.Message{Fallback: "x"})

	assert.Error(t, err)
}

func TestClient_MessageContent_ReadsBlocksAndText(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":true,"messages":[{"ts":"100.1","text":"please review PR #7","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"hi"}},{"type":"context","elements":[]}]}]}`))

	content, err := fake.client().MessageContent(context.Background(), "C123", "100.1")

	require.NoError(t, err)
	require.Len(t, content.Blocks, 2)
	assert.JSONEq(t, `{"type":"section","text":{"type":"mrkdwn","text":"hi"}}`, string(content.Blocks[0]))
	assert.Equal(t, "please review PR #7", content.Fallback)

	call := fake.lastCall(t)
	assert.Equal(t, http.MethodGet, call.Method)
	assert.Equal(t, "/api/conversations.history", call.Path)
	assert.Equal(t, []string{"C123"}, call.Query["channel"])
	assert.Equal(t, []string{"100.1"}, call.Query["latest"])
	assert.Equal(t, []string{"100.1"}, call.Query["oldest"])
	assert.Equal(t, []string{"true"}, call.Query["inclusive"])
	assert.Equal(t, []string{"1"}, call.Query["limit"])
}

func TestClient_MessageContent_MissingMessageIsSentinel(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":true,"messages":[]}`))

	_, err := fake.client().MessageContent(context.Background(), "C123", "100.1")

	require.ErrorIs(t, err, slack.ErrMessageNotFound)
}

func TestClient_PostMessageRawBlocks_SendsBlocksVerbatim(t *testing.T) {
	fake := newFakeSlack(t, okJSON(`{"ok":true,"ts":"200.2"}`))
	blocks := []json.RawMessage{json.RawMessage(`{"type":"section","text":{"type":"mrkdwn","text":"moved"}}`)}

	timestamp, err := fake.client().PostMessageRawBlocks(context.Background(), "C999", blocks, "moved")

	require.NoError(t, err)
	assert.Equal(t, "200.2", timestamp)

	call := fake.lastCall(t)
	assert.Equal(t, "/api/chat.postMessage", call.Path)
	payload := decodedBody(t, call)
	assert.Equal(t, "C999", payload["channel"])
	assert.Equal(t, "moved", payload["text"])
	assert.JSONEq(t, `[{"type":"section","text":{"type":"mrkdwn","text":"moved"}}]`, mustJSON(t, payload["blocks"]))
}

// A bulk relocate run legitimately hits Tier 3, so a ratelimited response is
// retried after the interval Slack asks for instead of failing the caller.
func TestClient_RetriesRateLimitedCall(t *testing.T) {
	var attempts int
	fake := newFakeSlack(t, func(string, []byte, map[string][]string) (int, string) {
		attempts++
		if attempts == 1 {
			return http.StatusTooManyRequests, `{"ok":false,"error":"ratelimited"}`
		}
		return http.StatusOK, `{"ok":true,"ts":"300.3"}`
	})
	fake.retryAfter = "0"

	timestamp, err := fake.client().PostMessage(context.Background(), "C1", slack.Message{Fallback: "hi"})

	require.NoError(t, err)
	assert.Equal(t, "300.3", timestamp)
	assert.Equal(t, 2, attempts, "the call is retried once, not abandoned")
}

func TestClient_RateLimitedGivesUpAfterMaxAttempts(t *testing.T) {
	var attempts int
	fake := newFakeSlack(t, func(string, []byte, map[string][]string) (int, string) {
		attempts++
		return http.StatusTooManyRequests, `{"ok":false,"error":"ratelimited"}`
	})
	fake.retryAfter = "0"

	_, err := fake.client().PostMessage(context.Background(), "C1", slack.Message{Fallback: "hi"})

	var apiErr *slack.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "ratelimited", apiErr.Code)
	assert.Equal(t, 3, attempts, "three attempts, then the error surfaces")
}
