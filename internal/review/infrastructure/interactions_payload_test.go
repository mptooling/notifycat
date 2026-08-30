package infrastructure

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// formEncode wraps a JSON interaction the way Slack does: a single,
// URL-encoded `payload` field in an application/x-www-form-urlencoded body.
func formEncode(jsonPayload string) []byte {
	return []byte("payload=" + url.QueryEscape(jsonPayload))
}

func TestParseInteraction_BlockActions(t *testing.T) {
	body := formEncode(`{
		"type": "block_actions",
		"user": {"id": "U123", "username": "alice"},
		"channel": {"id": "C999"},
		"message": {"ts": "1700000000.000100"},
		"response_url": "https://hooks.slack.com/actions/abc",
		"trigger_id": "trig-1",
		"actions": [{"action_id": "start_review", "value": "octo/widget#42"}]
	}`)

	interaction, err := ParseInteraction(body)

	require.NoError(t, err)
	assert.Equal(t, "block_actions", interaction.Type)
	assert.Equal(t, "U123", interaction.User.ID)
	assert.Equal(t, "alice", interaction.User.Username)
	assert.Equal(t, "C999", interaction.Channel.ID)
	assert.Equal(t, "1700000000.000100", interaction.Message.TS)
	assert.Equal(t, "https://hooks.slack.com/actions/abc", interaction.ResponseURL)
	assert.Equal(t, "trig-1", interaction.TriggerID)
	require.Len(t, interaction.Actions, 1)
	assert.Equal(t, "start_review", interaction.Actions[0].ActionID)
	assert.Equal(t, "octo/widget#42", interaction.Actions[0].Value)
}

func TestParseInteraction_MissingPayloadField(t *testing.T) {
	_, err := ParseInteraction([]byte("other=1"))

	assert.ErrorIs(t, err, ErrMissingPayload)
}

func TestParseInteraction_MalformedJSON(t *testing.T) {
	_, err := ParseInteraction(formEncode("not-json"))

	assert.Error(t, err)
}

func TestParseInteraction_CapturesMessageBlocksAndText(t *testing.T) {
	body := formEncode(`{"type":"block_actions","user":{"id":"U1","username":"ada"},"channel":{"id":"C1"},` +
		`"message":{"ts":"1.1","text":"please review","blocks":[{"type":"section"}]},` +
		`"actions":[{"action_id":"start_review","value":"octo/web#42"}]}`)

	got, err := ParseInteraction(body)

	require.NoError(t, err)
	assert.Equal(t, "please review", got.Message.Text)
	assert.Contains(t, string(got.Message.RawBlocks), `"section"`, "the blocks are kept raw for the re-render")
	assert.Equal(t, "octo/web#42", got.Actions[0].Value)
}

func TestParseInteraction_NoActions(t *testing.T) {
	// An interaction type we don't act on (e.g. a shortcut) still parses; the
	// handler decides what to ignore.
	interaction, err := ParseInteraction(formEncode(`{"type": "shortcut"}`))

	require.NoError(t, err)
	assert.Equal(t, "shortcut", interaction.Type)
	assert.Empty(t, interaction.Actions)
}
