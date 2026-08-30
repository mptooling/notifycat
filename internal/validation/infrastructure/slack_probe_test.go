package infrastructure

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/slack"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

// newProbe builds a SlackProbe whose client targets the given test server URL.
func newProbe(url string) *SlackProbe {
	return NewSlackProbe(slack.NewClient(http.DefaultClient, "xoxb-test", slack.WithBaseURL(url)))
}

// jsonServerProbe serves one canned Slack response and returns a probe for it.
func jsonServerProbe(t *testing.T, body string, header map[string]string) *SlackProbe {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for name, value := range header {
			w.Header().Set(name, value)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return newProbe(server.URL)
}

func TestSlackProbe_ConversationsInfo_MapsFields(t *testing.T) {
	probe := jsonServerProbe(t, `{"ok":true,"channel":{"id":"C1","name":"general","is_member":true,"is_archived":true}}`, nil)

	info, err := probe.ConversationsInfo(context.Background(), "C1")

	require.NoError(t, err)
	assert.Equal(t, domain.ChannelInfo{ID: "C1", Name: "general", IsMember: true, IsArchived: true}, info)
}

func TestSlackProbe_ConversationsInfo_TranslatesAPIError(t *testing.T) {
	probe := jsonServerProbe(t, `{"ok":false,"error":"channel_not_found"}`, nil)

	_, err := probe.ConversationsInfo(context.Background(), "C1")

	var apiErr *domain.SlackAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "channel_not_found", apiErr.Code)
	assert.Equal(t, "conversations.info", apiErr.Method)
}

func TestSlackProbe_AuthTest_TranslatesAPIError(t *testing.T) {
	probe := jsonServerProbe(t, `{"ok":false,"error":"invalid_auth"}`, nil)

	_, _, err := probe.AuthTest(context.Background())

	var apiErr *domain.SlackAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "invalid_auth", apiErr.Code)
}

func TestSlackProbe_AuthTest_ReturnsScopes(t *testing.T) {
	probe := jsonServerProbe(t, `{"ok":true,"user_id":"UBOT"}`, map[string]string{"X-OAuth-Scopes": "chat:write, reactions:write"})

	userID, scopes, err := probe.AuthTest(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "UBOT", userID)
	assert.Equal(t, []string{"chat:write", "reactions:write"}, scopes)
}

func TestTranslateSlackError_PassThrough(t *testing.T) {
	transportErr := errors.New("dial tcp: connection refused")

	assert.ErrorIs(t, translateSlackError(transportErr), transportErr, "a non-API error passes through untouched")
	assert.NoError(t, translateSlackError(nil))
}
