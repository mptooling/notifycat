package infrastructure

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/maintenance/domain"
	"github.com/mptooling/notifycat/internal/platform/slack"
)

// fakeSlackAPI answers each Slack method with a canned body and records the
// request bodies it received, keyed by method name.
type fakeSlackAPI struct {
	*httptest.Server
	bodies    map[string][]string
	responses map[string]string
}

func newFakeSlackAPI(t *testing.T, responses map[string]string) *fakeSlackAPI {
	t.Helper()

	fake := &fakeSlackAPI{bodies: map[string][]string{}, responses: responses}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		body, _ := io.ReadAll(r.Body)
		if r.Method == http.MethodGet {
			body = []byte(r.URL.RawQuery)
		}
		fake.bodies[method] = append(fake.bodies[method], string(body))

		response, ok := fake.responses[method]
		if !ok {
			response = `{"ok":true}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (f *fakeSlackAPI) courier() *SlackCourier {
	return NewSlackCourier(slack.NewClient(f.Client(), "xoxb-test", slack.WithBaseURL(f.URL)))
}

func TestSlackCourier_Repost_PostsMovedBlocksToDestination(t *testing.T) {
	fake := newFakeSlackAPI(t, map[string]string{
		"conversations.history": `{"ok":true,"messages":[{"text":"<!channel> please review PR #7","blocks":[{"type":"section","text":{"type":"mrkdwn","text":":new: <!channel> please review <https://github.com/acme/api/pull/7|PR #7: Add widgets>"}}]}]}`,
		"chat.postMessage":      `{"ok":true,"ts":"200.2"}`,
	})

	messageID, err := fake.courier().Repost(context.Background(),
		domain.TrackedMessage{Channel: "C_OLD", MessageID: "100.1"}, "C_NEW")

	require.NoError(t, err)
	assert.Equal(t, "200.2", messageID)

	require.Len(t, fake.bodies["chat.postMessage"], 1)
	var posted struct {
		Channel string            `json:"channel"`
		Text    string            `json:"text"`
		Blocks  []json.RawMessage `json:"blocks"`
	}
	require.NoError(t, json.Unmarshal([]byte(fake.bodies["chat.postMessage"][0]), &posted))
	assert.Equal(t, "C_NEW", posted.Channel)
	assert.Contains(t, string(posted.Blocks[0]), "moved from another channel")
	assert.NotContains(t, string(posted.Blocks[0]), "!channel", "a relocated message pings nobody")
	assert.NotContains(t, posted.Text, "!channel")
}

func TestSlackCourier_Repost_MissingOriginalIsMessageGone(t *testing.T) {
	fake := newFakeSlackAPI(t, map[string]string{
		"conversations.history": `{"ok":true,"messages":[]}`,
	})

	_, err := fake.courier().Repost(context.Background(),
		domain.TrackedMessage{Channel: "C_OLD", MessageID: "100.1"}, "C_NEW")

	require.ErrorIs(t, err, domain.ErrMessageGone)
}

func TestSlackCourier_CopyReactions_CarriesOnlyAllowedEmoji(t *testing.T) {
	fake := newFakeSlackAPI(t, map[string]string{
		"reactions.get": `{"ok":true,"message":{"reactions":[{"name":"eyes","count":1},{"name":"tada","count":3},{"name":"white_check_mark","count":1}]}}`,
	})

	err := fake.courier().CopyReactions(context.Background(),
		domain.TrackedMessage{Channel: "C_OLD", MessageID: "100.1"},
		domain.TrackedMessage{Channel: "C_NEW", MessageID: "200.2"},
		[]string{"eyes", "white_check_mark"})

	require.NoError(t, err)
	require.Len(t, fake.bodies["reactions.add"], 2, "the unlisted emoji is left behind")
	assert.Contains(t, fake.bodies["reactions.add"][0], `"name":"eyes"`)
	assert.Contains(t, fake.bodies["reactions.add"][0], `"channel":"C_NEW"`)
	assert.Contains(t, fake.bodies["reactions.add"][1], `"name":"white_check_mark"`)
}

func TestSlackCourier_Delete_TreatsAlreadyGoneAsDone(t *testing.T) {
	fake := newFakeSlackAPI(t, map[string]string{
		"chat.delete": `{"ok":false,"error":"message_not_found"}`,
	})

	err := fake.courier().Delete(context.Background(), domain.TrackedMessage{Channel: "C_OLD", MessageID: "100.1"})

	require.NoError(t, err)
}
