package infrastructure_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/access/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

var repoBaseTime = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func newRepos(t *testing.T) (*infrastructure.InviteRepo, *infrastructure.TokenRepo) {
	t.Helper()
	db := persistence.NewTestDB(t)
	return infrastructure.NewInviteRepo(persistence.NewAccessInvites(db)),
		infrastructure.NewTokenRepo(persistence.NewAccessTokens(db))
}

func storedInvite(inviteID string) domain.StoredInvite {
	return domain.StoredInvite{
		Invite: domain.Invite{
			ID:        inviteID,
			User:      "alice",
			ExpiresAt: repoBaseTime.Add(time.Hour),
			CreatedAt: repoBaseTime,
		},
		SecretHash: "invite-hash",
	}
}

func TestInviteRepo_RoundTripsStoredInvite(t *testing.T) {
	inviteRepo, _ := newRepos(t)
	ctx := context.Background()
	want := storedInvite("i1")

	require.NoError(t, inviteRepo.Create(ctx, want))
	got, err := inviteRepo.Find(ctx, "i1")

	require.NoError(t, err)
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.User, got.User)
	assert.Equal(t, want.SecretHash, got.SecretHash)
	assert.WithinDuration(t, want.ExpiresAt, got.ExpiresAt, time.Second)
	assert.Nil(t, got.UsedAt)
}

func TestInviteRepo_FindUnknownIsErrInviteNotFound(t *testing.T) {
	inviteRepo, _ := newRepos(t)

	_, err := inviteRepo.Find(context.Background(), "missing")

	assert.ErrorIs(t, err, domain.ErrInviteNotFound)
}

func TestInviteRepo_RedeemTwiceIsErrInviteUsed(t *testing.T) {
	inviteRepo, tokenRepo := newRepos(t)
	ctx := context.Background()
	require.NoError(t, inviteRepo.Create(ctx, storedInvite("i1")))
	firstToken := domain.StoredToken{Token: domain.Token{ID: "t1", User: "alice", CreatedAt: repoBaseTime}, SecretHash: "h"}
	secondToken := domain.StoredToken{Token: domain.Token{ID: "t2", User: "alice", CreatedAt: repoBaseTime}, SecretHash: "h"}
	require.NoError(t, inviteRepo.Redeem(ctx, "i1", firstToken))

	err := inviteRepo.Redeem(ctx, "i1", secondToken)

	assert.ErrorIs(t, err, domain.ErrInviteUsed)
	listed, listErr := tokenRepo.List(ctx)
	require.NoError(t, listErr)
	assert.Len(t, listed, 1)
}

func TestTokenRepo_UnknownIsErrTokenNotFound(t *testing.T) {
	_, tokenRepo := newRepos(t)
	ctx := context.Background()

	_, findErr := tokenRepo.Find(ctx, "missing")
	deleteErr := tokenRepo.Delete(ctx, "missing")

	assert.ErrorIs(t, findErr, domain.ErrTokenNotFound)
	assert.ErrorIs(t, deleteErr, domain.ErrTokenNotFound)
}
