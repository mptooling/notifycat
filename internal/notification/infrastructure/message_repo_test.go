package infrastructure

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

func TestMessageRepo_Messages_MapsRows(t *testing.T) {
	pullRequests := persistence.NewPullRequests(persistence.NewTestDB(t))
	repo := NewMessageRepo(pullRequests)
	require.NoError(t, pullRequests.AddMessage(context.Background(), "acme/api", 42, "C_ACME", "ts1"))

	got, err := repo.Messages(context.Background(), "acme/api", 42)

	require.NoError(t, err)
	assert.Equal(t, []domain.Message{{Channel: "C_ACME", MessageID: "ts1"}}, got)
}

func TestMessageRepo_Messages_UnknownPRReturnsNotFound(t *testing.T) {
	repo := NewMessageRepo(persistence.NewPullRequests(persistence.NewTestDB(t)))

	_, err := repo.Messages(context.Background(), "ghost/x", 1)

	assert.ErrorIs(t, err, routingdomain.ErrNotFound)
}
