package persistence_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

var accessBaseTime = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func newInvite(inviteID string) persistence.AccessInvite {
	return persistence.AccessInvite{
		ID:         inviteID,
		User:       "alice",
		SecretHash: "invite-hash",
		ExpiresAt:  accessBaseTime.Add(time.Hour),
		CreatedAt:  accessBaseTime,
	}
}

func seedToken(t *testing.T, db *gorm.DB, tokenID string, createdAt time.Time) {
	t.Helper()
	invites := persistence.NewAccessInvites(db)
	require.NoError(t, invites.Create(context.Background(), newInvite("invite-"+tokenID)))
	token := persistence.AccessToken{ID: tokenID, User: "alice", SecretHash: "token-hash", CreatedAt: createdAt}
	require.NoError(t, invites.Redeem(context.Background(), "invite-"+tokenID, token))
}

func TestAccessInvites_CreateAndFind(t *testing.T) {
	invites := persistence.NewAccessInvites(persistence.NewTestDB(t))
	ctx := context.Background()
	want := newInvite("i1")

	require.NoError(t, invites.Create(ctx, want))
	got, err := invites.Find(ctx, "i1")

	require.NoError(t, err)
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.User, got.User)
	assert.Equal(t, want.SecretHash, got.SecretHash)
	assert.WithinDuration(t, want.ExpiresAt, got.ExpiresAt, time.Second)
	assert.WithinDuration(t, want.CreatedAt, got.CreatedAt, time.Second)
	assert.Nil(t, got.UsedAt)
}

func TestAccessInvites_Find_UnknownIsErrNotFound(t *testing.T) {
	invites := persistence.NewAccessInvites(persistence.NewTestDB(t))

	_, err := invites.Find(context.Background(), "missing")

	assert.ErrorIs(t, err, persistence.ErrNotFound)
}

func TestAccessInvites_Redeem_MarksUsedAndCreatesToken(t *testing.T) {
	db := persistence.NewTestDB(t)
	invites := persistence.NewAccessInvites(db)
	tokens := persistence.NewAccessTokens(db)
	ctx := context.Background()
	require.NoError(t, invites.Create(ctx, newInvite("i1")))
	token := persistence.AccessToken{ID: "t1", User: "alice", SecretHash: "token-hash", CreatedAt: accessBaseTime.Add(time.Minute)}

	require.NoError(t, invites.Redeem(ctx, "i1", token))

	invite, err := invites.Find(ctx, "i1")
	require.NoError(t, err)
	require.NotNil(t, invite.UsedAt)
	assert.WithinDuration(t, token.CreatedAt, *invite.UsedAt, time.Second)
	stored, err := tokens.Find(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, "alice", stored.User)
	assert.Equal(t, "token-hash", stored.SecretHash)
}

func TestAccessInvites_Redeem_UsedInviteIsErrInviteUsed(t *testing.T) {
	db := persistence.NewTestDB(t)
	invites := persistence.NewAccessInvites(db)
	tokens := persistence.NewAccessTokens(db)
	ctx := context.Background()
	seedToken(t, db, "t1", accessBaseTime)

	err := invites.Redeem(ctx, "invite-t1", persistence.AccessToken{ID: "t2", User: "alice", SecretHash: "h", CreatedAt: accessBaseTime})

	require.ErrorIs(t, err, persistence.ErrInviteUsed)
	_, findErr := tokens.Find(ctx, "t2")
	assert.ErrorIs(t, findErr, persistence.ErrNotFound)
}

func TestAccessInvites_Redeem_RollsBackWhenTokenInsertFails(t *testing.T) {
	db := persistence.NewTestDB(t)
	invites := persistence.NewAccessInvites(db)
	ctx := context.Background()
	seedToken(t, db, "t1", accessBaseTime)
	require.NoError(t, invites.Create(ctx, newInvite("invite-b")))

	err := invites.Redeem(ctx, "invite-b", persistence.AccessToken{ID: "t1", User: "alice", SecretHash: "h", CreatedAt: accessBaseTime})

	require.Error(t, err)
	invite, findErr := invites.Find(ctx, "invite-b")
	require.NoError(t, findErr)
	assert.Nil(t, invite.UsedAt, "a failed token insert must not consume the invite")
}

func TestAccessTokens_ListOrdersByCreatedAt(t *testing.T) {
	db := persistence.NewTestDB(t)
	tokens := persistence.NewAccessTokens(db)
	seedToken(t, db, "newer", accessBaseTime.Add(time.Hour))
	seedToken(t, db, "older", accessBaseTime)

	listed, err := tokens.List(context.Background())

	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, "older", listed[0].ID)
	assert.Equal(t, "newer", listed[1].ID)
}

func TestAccessTokens_List_EmptyIsEmpty(t *testing.T) {
	tokens := persistence.NewAccessTokens(persistence.NewTestDB(t))

	listed, err := tokens.List(context.Background())

	require.NoError(t, err)
	assert.Empty(t, listed)
}

func TestAccessTokens_Delete(t *testing.T) {
	db := persistence.NewTestDB(t)
	tokens := persistence.NewAccessTokens(db)
	ctx := context.Background()
	seedToken(t, db, "t1", accessBaseTime)

	require.NoError(t, tokens.Delete(ctx, "t1"))

	_, findErr := tokens.Find(ctx, "t1")
	assert.ErrorIs(t, findErr, persistence.ErrNotFound)
	assert.ErrorIs(t, tokens.Delete(ctx, "t1"), persistence.ErrNotFound)
}

func TestAccessTokens_TouchLastUsed(t *testing.T) {
	db := persistence.NewTestDB(t)
	tokens := persistence.NewAccessTokens(db)
	ctx := context.Background()
	seedToken(t, db, "t1", accessBaseTime)
	usedAt := accessBaseTime.Add(2 * time.Hour)

	require.NoError(t, tokens.TouchLastUsed(ctx, "t1", usedAt))

	stored, err := tokens.Find(ctx, "t1")
	require.NoError(t, err)
	require.NotNil(t, stored.LastUsedAt)
	assert.WithinDuration(t, usedAt, *stored.LastUsedAt, time.Second)
}

func TestMigration00010_DownDropsAccessTables(t *testing.T) {
	db := persistence.NewTestDB(t)
	ctx := context.Background()
	const countSQL = "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('access_invites','access_tokens')"
	var tableCount int

	require.NoError(t, persistence.MigrateDown(ctx, db))
	require.NoError(t, db.Raw(countSQL).Scan(&tableCount).Error)
	assert.Zero(t, tableCount)

	require.NoError(t, persistence.MigrateUp(ctx, db))
	require.NoError(t, db.Raw(countSQL).Scan(&tableCount).Error)
	assert.Equal(t, 2, tableCount)
}
