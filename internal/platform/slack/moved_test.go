package slack_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/slack"
)

func rawBlocks(t *testing.T, blocks ...string) []json.RawMessage {
	t.Helper()

	out := make([]json.RawMessage, len(blocks))
	for i, block := range blocks {
		out[i] = json.RawMessage(block)
	}
	return out
}

// headlineText returns the mrkdwn text of a section block.
func headlineText(t *testing.T, block json.RawMessage) string {
	t.Helper()

	var decoded struct {
		Type string `json:"type"`
		Text struct {
			Text string `json:"text"`
		} `json:"text"`
	}
	require.NoError(t, json.Unmarshal(block, &decoded))
	require.Equal(t, "section", decoded.Type)
	return decoded.Text.Text
}

func TestMovedMessage_ReplacesHeadlineAndDropsMentions(t *testing.T) {
	content := slack.RawMessageContent{
		Blocks: rawBlocks(t,
			`{"type":"section","text":{"type":"mrkdwn","text":":new: <!subteam^S0ENG>, <@U1>, please review <https://github.com/acme/api/pull/7|PR #7: Add widgets>"}}`,
			`{"type":"context","elements":[{"type":"mrkdwn","text":"acme/api · by octocat"}]}`,
		),
		Fallback: "<!subteam^S0ENG>, please review PR #7: Add widgets by octocat",
	}

	moved, err := slack.MovedMessage(content)

	require.NoError(t, err)
	require.Len(t, moved.Blocks, 2)
	assert.Equal(t,
		":truck: [moved from another channel] please review <https://github.com/acme/api/pull/7|PR #7: Add widgets>",
		headlineText(t, moved.Blocks[0]))
	assert.NotContains(t, headlineText(t, moved.Blocks[0]), "subteam", "a moved message never pings")
	assert.Equal(t, "[moved from another channel] please review PR #7: Add widgets", moved.Fallback)
	assert.NotContains(t, moved.Fallback, "subteam")
}

func TestMovedMessage_KeepsEveryOtherBlockVerbatim(t *testing.T) {
	context := `{"type":"context","elements":[{"type":"mrkdwn","text":":eye: <@U2> reviewing"}]}`
	actions := `{"type":"actions","elements":[{"type":"button","action_id":"start_review","value":"acme/api#7"}]}`
	content := slack.RawMessageContent{
		Blocks: rawBlocks(t,
			`{"type":"section","text":{"type":"mrkdwn","text":":new: please review <https://github.com/acme/api/pull/7|PR #7: Add widgets>"}}`,
			context, actions,
		),
		Fallback: "please review PR #7",
	}

	moved, err := slack.MovedMessage(content)

	require.NoError(t, err)
	require.Len(t, moved.Blocks, 3)
	assert.JSONEq(t, context, string(moved.Blocks[1]), "review markers survive the move")
	assert.JSONEq(t, actions, string(moved.Blocks[2]), "the Start review button survives the move")
}

func TestMovedMessage_HandlesBotHeadline(t *testing.T) {
	content := slack.RawMessageContent{
		Blocks: rawBlocks(t,
			`{"type":"section","text":{"type":"mrkdwn","text":":package: <!channel> dependabot bumped <https://github.com/acme/api/pull/9|PR #9: bump lodash>"}}`,
		),
		Fallback: "<!channel> dependabot bumped PR #9: bump lodash",
	}

	moved, err := slack.MovedMessage(content)

	require.NoError(t, err)
	assert.Equal(t,
		":truck: [moved from another channel] please review <https://github.com/acme/api/pull/9|PR #9: bump lodash>",
		headlineText(t, moved.Blocks[0]))
}

// Rewriting a headline we cannot parse risks carrying its mentions along, so an
// unrecognized shape is refused rather than guessed at.
func TestMovedMessage_RefusesUnexpectedShape(t *testing.T) {
	testCases := []struct {
		name   string
		blocks []json.RawMessage
	}{
		{name: "no blocks"},
		{
			name:   "headline is not a section",
			blocks: rawBlocks(t, `{"type":"context","elements":[]}`),
		},
		{
			name:   "headline carries no PR link",
			blocks: rawBlocks(t, `{"type":"section","text":{"type":"mrkdwn","text":":new: <!channel> please review"}}`),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := slack.MovedMessage(slack.RawMessageContent{Blocks: testCase.blocks})

			require.ErrorIs(t, err, slack.ErrUnexpectedMessageShape)
		})
	}
}
