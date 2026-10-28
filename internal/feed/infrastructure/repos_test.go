package infrastructure_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/feed/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

var repoBaseTime = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func newFeedDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := persistence.NewTestDB(t)
	invites := persistence.NewAccessInvites(db)
	invite := persistence.AccessInvite{
		ID: "invite-t1", User: "alice", SecretHash: "invite-hash",
		ExpiresAt: repoBaseTime.Add(time.Hour), CreatedAt: repoBaseTime,
	}
	require.NoError(t, invites.Create(context.Background(), invite))
	token := persistence.AccessToken{ID: "t1", User: "alice", SecretHash: "token-hash", CreatedAt: repoBaseTime}
	require.NoError(t, invites.Redeem(context.Background(), "invite-t1", token))
	return db
}

func fullPullRequest() domain.PullRequest {
	return domain.PullRequest{
		Repository:   "acme/api",
		Number:       7,
		Title:        "Add feed",
		Description:  "body",
		URL:          "https://example.test/acme/api/pull/7",
		AuthorID:     "42",
		SourceBranch: "feature",
		TargetBranch: "main",
		HeadSHA:      "abc123",
		CreatedAt:    repoBaseTime.Add(-time.Hour),
		UpdatedAt:    repoBaseTime,
	}
}

func TestPullRequestRepo_RoundTripsEveryField(t *testing.T) {
	repo := infrastructure.NewPullRequestRepo(persistence.NewFeedPullRequests(newFeedDB(t)))
	ctx := context.Background()
	want := fullPullRequest()

	require.NoError(t, repo.Upsert(ctx, want))
	got, err := repo.ChangedSince(ctx, []string{"acme/api"}, repoBaseTime.Add(-time.Minute))

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, want.Repository, got[0].Repository)
	assert.Equal(t, want.Number, got[0].Number)
	assert.Equal(t, want.Title, got[0].Title)
	assert.Equal(t, want.Description, got[0].Description)
	assert.Equal(t, want.URL, got[0].URL)
	assert.Equal(t, want.AuthorID, got[0].AuthorID)
	assert.Equal(t, want.SourceBranch, got[0].SourceBranch)
	assert.Equal(t, want.TargetBranch, got[0].TargetBranch)
	assert.Equal(t, want.HeadSHA, got[0].HeadSHA)
	assert.WithinDuration(t, want.CreatedAt, got[0].CreatedAt, time.Second)
	assert.WithinDuration(t, want.UpdatedAt, got[0].UpdatedAt, time.Second)
}

func TestPullRequestRepo_ZeroCreatedAtRoundTripsAsZero(t *testing.T) {
	repo := infrastructure.NewPullRequestRepo(persistence.NewFeedPullRequests(newFeedDB(t)))
	ctx := context.Background()
	pullRequest := fullPullRequest()
	pullRequest.CreatedAt = time.Time{}

	require.NoError(t, repo.Upsert(ctx, pullRequest))
	got, err := repo.ChangedSince(ctx, []string{"acme/api"}, repoBaseTime.Add(-time.Minute))

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].CreatedAt.IsZero(), "an unknown creation time stays unknown")
}

func TestPullRequestRepo_RepositoriesAndDelete(t *testing.T) {
	repo := infrastructure.NewPullRequestRepo(persistence.NewFeedPullRequests(newFeedDB(t)))
	ctx := context.Background()
	require.NoError(t, repo.Upsert(ctx, fullPullRequest()))

	repositories, listErr := repo.Repositories(ctx)
	deleteErr := repo.Delete(ctx, "acme/api", 7)
	remaining, remainingErr := repo.Repositories(ctx)

	require.NoError(t, listErr)
	require.NoError(t, deleteErr)
	require.NoError(t, remainingErr)
	assert.Equal(t, []string{"acme/api"}, repositories)
	assert.Empty(t, remaining)
}

func TestSubscriptionRepo_AddListDelete(t *testing.T) {
	repo := infrastructure.NewSubscriptionRepo(persistence.NewFeedSubscriptions(newFeedDB(t)))
	ctx := context.Background()

	added, addErr := repo.Add(ctx, domain.NewSubscriptions{
		TokenID: "t1", Repositories: []string{"acme/api", "acme/web"}, CreatedAt: repoBaseTime,
	})
	listed, listErr := repo.List(ctx, "t1")

	require.NoError(t, addErr)
	require.NoError(t, listErr)
	require.Len(t, listed, 2)
	assert.Equal(t, 2, added)
	assert.Equal(t, "t1", listed[0].TokenID)

	removed, removeErr := repo.Delete(ctx, "t1", listed[0].ID)

	require.NoError(t, removeErr)
	assert.Equal(t, listed[0].ID, removed.ID)
	assert.Equal(t, listed[0].Repository, removed.Repository)
}

func TestSubscriptionRepo_DeleteUnknownIsErrSubscriptionNotFound(t *testing.T) {
	repo := infrastructure.NewSubscriptionRepo(persistence.NewFeedSubscriptions(newFeedDB(t)))

	_, err := repo.Delete(context.Background(), "t1", 999)

	assert.ErrorIs(t, err, domain.ErrSubscriptionNotFound)
}

func TestDeliveryRepo_ClaimReturnsDomainPullRequestsOnce(t *testing.T) {
	db := newFeedDB(t)
	pullRequests := infrastructure.NewPullRequestRepo(persistence.NewFeedPullRequests(db))
	deliveries := infrastructure.NewDeliveryRepo(persistence.NewFeedDeliveries(db))
	ctx := context.Background()
	want := fullPullRequest()
	require.NoError(t, pullRequests.Upsert(ctx, want))
	claim := domain.Claim{TokenID: "t1", Repositories: []string{"acme/api"}, DeliveredAt: repoBaseTime}

	first, firstErr := deliveries.Claim(ctx, claim)
	second, secondErr := deliveries.Claim(ctx, claim)

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.Len(t, first, 1)
	assert.Equal(t, want.Number, first[0].Number)
	assert.Equal(t, want.HeadSHA, first[0].HeadSHA)
	assert.Empty(t, second, "a pull request is delivered at most once")
}

func TestSystemClock_NowIsUTC(t *testing.T) {
	now := infrastructure.NewSystemClock().Now()

	assert.Equal(t, time.UTC, now.Location())
	assert.WithinDuration(t, time.Now(), now, time.Minute)
}
