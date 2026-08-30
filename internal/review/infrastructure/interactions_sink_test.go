package infrastructure

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	reviewdomain "github.com/mptooling/notifycat/internal/review/domain"
)

type fakeStartReview struct {
	called  bool
	command reviewdomain.StartReviewCommand
	err     error
}

func (f *fakeStartReview) Handle(_ context.Context, command reviewdomain.StartReviewCommand) error {
	f.called = true
	f.command = command
	return f.err
}

func TestStartReviewSink_HappyPath_ForwardsCommand(t *testing.T) {
	startReview := &fakeStartReview{}
	sink := NewStartReviewSink(startReview, discardLogger())
	rawBlocks := json.RawMessage(`[{"type":"section"}]`)
	interaction := Interaction{
		Type:    "block_actions",
		User:    User{ID: "U1", Username: "ada"},
		Channel: Channel{ID: "C1"},
		Message: Message{TS: "1.1", Text: "please review", RawBlocks: rawBlocks},
		Actions: []Action{{ActionID: "start_review", Value: "octo/web#42"}},
	}

	err := sink(context.Background(), interaction)

	require.NoError(t, err)
	require.True(t, startReview.called)
	command := startReview.command
	assert.Equal(t, "octo/web", command.Repository)
	assert.Equal(t, 42, command.PRNumber)
	assert.Equal(t, reviewdomain.Reviewer{UserID: "U1", UserName: "ada"}, command.Reviewer)
	assert.Equal(t, "C1", command.Message.Channel)
	assert.Equal(t, "1.1", command.Message.TS)
	assert.Equal(t, "please review", command.Message.Fallback)
	assert.JSONEq(t, string(rawBlocks), string(command.Message.RawBlocks))
}

func TestStartReviewSink_IgnoresInteractionsItDoesNotOwn(t *testing.T) {
	testCases := []struct {
		name        string
		interaction Interaction
	}{
		{
			name: "wrong interaction type",
			interaction: Interaction{
				Type:    "shortcut",
				Actions: []Action{{ActionID: "start_review", Value: "octo/web#42"}},
			},
		},
		{
			name: "wrong action id",
			interaction: Interaction{
				Type:    "block_actions",
				Actions: []Action{{ActionID: "something_else", Value: "octo/web#42"}},
			},
		},
		{
			name: "malformed action value",
			interaction: Interaction{
				Type:    "block_actions",
				Actions: []Action{{ActionID: "start_review", Value: "no-hash"}},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			startReview := &fakeStartReview{}
			sink := NewStartReviewSink(startReview, discardLogger())

			err := sink(context.Background(), testCase.interaction)

			require.NoError(t, err)
			assert.False(t, startReview.called)
		})
	}
}
