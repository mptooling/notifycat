package infrastructure

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func TestMessageChecker_HasMessages_TrueForSeededPR(t *testing.T) {
	ctx := context.Background()
	pullRequests := persistence.NewPullRequests(persistence.NewTestDB(t))
	checker := NewMessageChecker(pullRequests)
	require.NoError(t, pullRequests.AddMessage(ctx, "octo/widget", 10, "C001", "ts-1"))

	hasMessages, err := checker.HasMessages(ctx, "octo/widget", 10)

	require.NoError(t, err)
	assert.True(t, hasMessages)
}

func TestMessageChecker_HasMessages_FalseForUntrackedPR(t *testing.T) {
	checker := NewMessageChecker(persistence.NewPullRequests(persistence.NewTestDB(t)))

	hasMessages, err := checker.HasMessages(context.Background(), "octo/widget", 99)

	require.NoError(t, err)
	assert.False(t, hasMessages)
}
