package persistence_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func newFeedDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := persistence.NewTestDB(t)
	seedToken(t, db, "t1", accessBaseTime)
	seedToken(t, db, "t2", accessBaseTime)
	return db
}

func feedPullRequest(repository string, number int, updatedAt time.Time) persistence.FeedPullRequest {
	return persistence.FeedPullRequest{
		Repository: repository,
		Number:     number,
		Title:      fmt.Sprintf("PR %d", number),
		URL:        fmt.Sprintf("https://github.com/%s/pull/%d", repository, number),
		HeadSHA:    "sha-1",
		UpdatedAt:  updatedAt,
	}
}

func upsertAll(t *testing.T, pullRequests *persistence.FeedPullRequests, items ...persistence.FeedPullRequest) {
	t.Helper()
	for _, item := range items {
		require.NoError(t, pullRequests.Upsert(context.Background(), item))
	}
}

func pullRequestNumbers(items []persistence.FeedPullRequest) []int {
	numbers := make([]int, 0, len(items))
	for _, item := range items {
		numbers = append(numbers, item.Number)
	}
	return numbers
}

func countTable(t *testing.T, db *gorm.DB, table string, where string, args ...any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Table(table).Where(where, args...).Count(&count).Error)
	return count
}

func TestFeedSubscriptions_AddKeepsExisting(t *testing.T) {
	subscriptions := persistence.NewFeedSubscriptions(newFeedDB(t))
	ctx := context.Background()

	first, err := subscriptions.Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID: "t1", Repositories: []string{"acme/api", "acme/web"}, CreatedAt: accessBaseTime,
	})
	require.NoError(t, err)
	second, err := subscriptions.Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID: "t1", Repositories: []string{"acme/web", "acme/ops"}, CreatedAt: accessBaseTime,
	})
	require.NoError(t, err)
	listed, err := subscriptions.List(ctx, "t1")

	require.NoError(t, err)
	assert.Equal(t, 2, first)
	assert.Equal(t, 1, second)
	repositories := make([]string, 0, len(listed))
	for _, subscription := range listed {
		repositories = append(repositories, subscription.Repository)
	}
	assert.Equal(t, []string{"acme/api", "acme/web", "acme/ops"}, repositories)
}

func TestFeedSubscriptions_AddEmptyBatch(t *testing.T) {
	subscriptions := persistence.NewFeedSubscriptions(newFeedDB(t))

	added, err := subscriptions.Add(context.Background(), persistence.FeedSubscriptionBatch{TokenID: "t1"})

	require.NoError(t, err)
	assert.Zero(t, added)
}

func TestFeedSubscriptions_DeleteOnlyOwn(t *testing.T) {
	subscriptions := persistence.NewFeedSubscriptions(newFeedDB(t))
	ctx := context.Background()
	_, err := subscriptions.Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID: "t1", Repositories: []string{"acme/api"}, CreatedAt: accessBaseTime,
	})
	require.NoError(t, err)
	listed, err := subscriptions.List(ctx, "t1")
	require.NoError(t, err)
	require.Len(t, listed, 1)

	_, foreignErr := subscriptions.Delete(ctx, "t2", listed[0].ID)
	stillListed, err := subscriptions.List(ctx, "t1")
	require.NoError(t, err)
	deleted, ownErr := subscriptions.Delete(ctx, "t1", listed[0].ID)
	afterwards, err := subscriptions.List(ctx, "t1")
	require.NoError(t, err)

	assert.ErrorIs(t, foreignErr, persistence.ErrNotFound)
	assert.Len(t, stillListed, 1)
	require.NoError(t, ownErr)
	assert.Equal(t, "acme/api", deleted.Repository)
	assert.Empty(t, afterwards)
}

func TestFeedTables_TokenDeleteCascades(t *testing.T) {
	db := newFeedDB(t)
	ctx := context.Background()
	subscriptions := persistence.NewFeedSubscriptions(db)
	pullRequests := persistence.NewFeedPullRequests(db)
	deliveries := persistence.NewFeedDeliveries(db)
	_, err := subscriptions.Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID: "t1", Repositories: []string{"acme/api"}, CreatedAt: accessBaseTime,
	})
	require.NoError(t, err)
	upsertAll(t, pullRequests, feedPullRequest("acme/api", 1, accessBaseTime))
	_, err = deliveries.Claim(ctx, persistence.FeedClaim{TokenID: "t1", Repositories: []string{"acme/api"}, DeliveredAt: accessBaseTime})
	require.NoError(t, err)
	require.EqualValues(t, 1, countTable(t, db, "feed_subscriptions", "token_id = ?", "t1"))
	require.EqualValues(t, 1, countTable(t, db, "feed_deliveries", "token_id = ?", "t1"))

	require.NoError(t, persistence.NewAccessTokens(db).Delete(ctx, "t1"))

	assert.Zero(t, countTable(t, db, "feed_subscriptions", "token_id = ?", "t1"))
	assert.Zero(t, countTable(t, db, "feed_deliveries", "token_id = ?", "t1"))
}

func TestFeedPullRequests_UpsertUpdatesInPlaceAndKeepsDeliveries(t *testing.T) {
	db := newFeedDB(t)
	ctx := context.Background()
	pullRequests := persistence.NewFeedPullRequests(db)
	deliveries := persistence.NewFeedDeliveries(db)
	upsertAll(t, pullRequests, feedPullRequest("acme/api", 42, accessBaseTime))
	_, err := deliveries.Claim(ctx, persistence.FeedClaim{TokenID: "t1", Repositories: []string{"acme/api"}, DeliveredAt: accessBaseTime})
	require.NoError(t, err)
	updated := feedPullRequest("acme/api", 42, accessBaseTime.Add(time.Hour))
	updated.HeadSHA = "sha-2"
	updated.Title = "renamed"

	require.NoError(t, pullRequests.Upsert(ctx, updated))

	stored, err := pullRequests.ChangedSince(ctx, []string{"acme/api"}, accessBaseTime.Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "sha-2", stored[0].HeadSHA)
	assert.Equal(t, "renamed", stored[0].Title)
	assert.EqualValues(t, 1, countTable(t, db, "feed_deliveries", "token_id = ?", "t1"),
		"an upsert must not replace the row and cascade its deliveries")
}

func TestFeedPullRequests_UpsertWithoutCreatedAtKeepsStoredCreatedAt(t *testing.T) {
	db := newFeedDB(t)
	ctx := context.Background()
	pullRequests := persistence.NewFeedPullRequests(db)
	openedAt := accessBaseTime.Add(-time.Hour)
	opened := feedPullRequest("acme/api", 42, accessBaseTime)
	opened.CreatedAt = &openedAt
	upsertAll(t, pullRequests, opened)

	require.NoError(t, pullRequests.Upsert(ctx, feedPullRequest("acme/api", 42, accessBaseTime.Add(time.Hour))))

	stored, err := pullRequests.ChangedSince(ctx, []string{"acme/api"}, accessBaseTime.Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.NotNil(t, stored[0].CreatedAt, "an event without a creation time keeps the stored one")
	assert.True(t, openedAt.Equal(*stored[0].CreatedAt))
}

func TestFeedPullRequests_DeleteCascadesDeliveries(t *testing.T) {
	db := newFeedDB(t)
	ctx := context.Background()
	pullRequests := persistence.NewFeedPullRequests(db)
	upsertAll(t, pullRequests, feedPullRequest("acme/api", 42, accessBaseTime))
	claimed, err := persistence.NewFeedDeliveries(db).Claim(ctx, persistence.FeedClaim{
		TokenID: "t1", Repositories: []string{"acme/api"}, DeliveredAt: accessBaseTime,
	})
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	deleteErr := pullRequests.Delete(ctx, "acme/api", 42)
	missingErr := pullRequests.Delete(ctx, "acme/api", 999)

	require.NoError(t, deleteErr)
	assert.NoError(t, missingErr)
	assert.Zero(t, countTable(t, db, "feed_deliveries", "token_id = ?", "t1"))
}

func seedDeliveredFeed(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	subscriptions := persistence.NewFeedSubscriptions(db)
	_, err := subscriptions.Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID: "t1", Repositories: []string{"acme/api", "acme/web"}, CreatedAt: accessBaseTime,
	})
	require.NoError(t, err)
	upsertAll(t, persistence.NewFeedPullRequests(db),
		feedPullRequest("acme/api", 1, accessBaseTime),
		feedPullRequest("acme/api", 2, accessBaseTime),
		feedPullRequest("acme/web", 3, accessBaseTime),
	)
	_, err = persistence.NewFeedDeliveries(db).Claim(ctx, persistence.FeedClaim{
		TokenID: "t1", Repositories: []string{"acme/api", "acme/web"}, DeliveredAt: accessBaseTime,
	})
	require.NoError(t, err)
}

func TestFeedPullRequests_DeleteAllCascadesDeliveriesAndKeepsSubscriptions(t *testing.T) {
	db := newFeedDB(t)
	seedDeliveredFeed(t, db)

	removed, err := persistence.NewFeedPullRequests(db).DeleteAll(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 3, removed)
	assert.Zero(t, countTable(t, db, "feed_pull_requests", "1 = 1"))
	assert.Zero(t, countTable(t, db, "feed_deliveries", "1 = 1"))
	assert.EqualValues(t, 2, countTable(t, db, "feed_subscriptions", "token_id = ?", "t1"))
	assert.EqualValues(t, 1, countTable(t, db, "access_tokens", "id = ?", "t1"))
}

func seedSubscriptionsOfTwoTokens(t *testing.T, subscriptions *persistence.FeedSubscriptions) {
	t.Helper()
	for _, tokenID := range []string{"t1", "t2"} {
		_, err := subscriptions.Add(context.Background(), persistence.FeedSubscriptionBatch{
			TokenID: tokenID, Repositories: []string{"acme/web", "acme/api", "*"}, CreatedAt: accessBaseTime,
		})
		require.NoError(t, err)
	}
}

func TestFeedSubscriptions_RepositoriesAreDistinct(t *testing.T) {
	subscriptions := persistence.NewFeedSubscriptions(newFeedDB(t))
	seedSubscriptionsOfTwoTokens(t, subscriptions)

	repositories, err := subscriptions.Repositories(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"*", "acme/api", "acme/web"}, repositories, "two tokens on one repository list it once")
}

func TestFeedSubscriptions_DeleteRepositoriesRemovesEveryTokensRows(t *testing.T) {
	db := newFeedDB(t)
	subscriptions := persistence.NewFeedSubscriptions(db)
	seedSubscriptionsOfTwoTokens(t, subscriptions)

	removed, err := subscriptions.DeleteRepositories(context.Background(), []string{"acme/api", "acme/gone"})

	require.NoError(t, err)
	assert.Equal(t, 2, removed)
	assert.Zero(t, countTable(t, db, "feed_subscriptions", "repository = ?", "acme/api"))
	assert.EqualValues(t, 2, countTable(t, db, "feed_subscriptions", "repository = ?", "acme/web"))
	assert.EqualValues(t, 2, countTable(t, db, "feed_subscriptions", "repository = ?", "*"))
}

func TestFeedSubscriptions_DeleteRepositoriesWithoutRepositories(t *testing.T) {
	db := newFeedDB(t)
	subscriptions := persistence.NewFeedSubscriptions(db)
	seedSubscriptionsOfTwoTokens(t, subscriptions)

	removed, err := subscriptions.DeleteRepositories(context.Background(), nil)

	require.NoError(t, err)
	assert.Zero(t, removed)
	assert.EqualValues(t, 6, countTable(t, db, "feed_subscriptions", "1 = 1"))
}

func TestFeedPullRequests_DeleteRepositoriesKeepsOtherRepositories(t *testing.T) {
	db := newFeedDB(t)
	seedDeliveredFeed(t, db)
	pullRequests := persistence.NewFeedPullRequests(db)

	removed, err := pullRequests.DeleteRepositories(context.Background(), []string{"acme/api", "acme/gone"})

	require.NoError(t, err)
	assert.Equal(t, 2, removed)
	assert.Zero(t, countTable(t, db, "feed_pull_requests", "repository = ?", "acme/api"))
	assert.Zero(t, countTable(t, db, "feed_deliveries", "repository = ?", "acme/api"))
	assert.EqualValues(t, 1, countTable(t, db, "feed_pull_requests", "repository = ?", "acme/web"))
	assert.EqualValues(t, 1, countTable(t, db, "feed_deliveries", "repository = ?", "acme/web"))
	assert.EqualValues(t, 2, countTable(t, db, "feed_subscriptions", "token_id = ?", "t1"))
}

func TestFeedPullRequests_DeleteRepositoriesWithoutRepositories(t *testing.T) {
	db := newFeedDB(t)
	seedDeliveredFeed(t, db)

	removed, err := persistence.NewFeedPullRequests(db).DeleteRepositories(context.Background(), nil)

	require.NoError(t, err)
	assert.Zero(t, removed)
	assert.EqualValues(t, 3, countTable(t, db, "feed_pull_requests", "1 = 1"))
}

func TestFeedPullRequests_ChangedSince(t *testing.T) {
	pullRequests := persistence.NewFeedPullRequests(newFeedDB(t))
	ctx := context.Background()
	atHour := func(hour int) time.Time { return time.Date(2026, 10, 28, hour, 0, 0, 0, time.UTC) }
	upsertAll(t, pullRequests,
		feedPullRequest("acme/api", 1, atHour(9)),
		feedPullRequest("acme/api", 3, atHour(10)),
		feedPullRequest("acme/api", 2, atHour(11)),
		feedPullRequest("acme/web", 4, atHour(11)),
	)
	offset := time.FixedZone("plus2", 2*60*60)

	utcResult, utcErr := pullRequests.ChangedSince(ctx, []string{"acme/api"}, atHour(10))
	offsetResult, offsetErr := pullRequests.ChangedSince(ctx, []string{"acme/api"}, time.Date(2026, 10, 28, 12, 0, 0, 0, offset))
	emptyResult, emptyErr := pullRequests.ChangedSince(ctx, nil, atHour(0))

	require.NoError(t, utcErr)
	require.NoError(t, offsetErr)
	require.NoError(t, emptyErr)
	assert.Equal(t, []int{3, 2}, pullRequestNumbers(utcResult), "the boundary is included, ordered by updated_at")
	assert.Equal(t, []int{3, 2}, pullRequestNumbers(offsetResult), "an offset since is compared in UTC")
	assert.Empty(t, emptyResult)
}

func TestFeedPullRequests_UpsertStoresNonUTCTimesInUTC(t *testing.T) {
	pullRequests := persistence.NewFeedPullRequests(newFeedDB(t))
	ctx := context.Background()
	offset := time.FixedZone("plus2", 2*60*60)
	upsertAll(t, pullRequests, feedPullRequest("acme/api", 1, time.Date(2026, 10, 28, 12, 0, 0, 0, offset)))

	atBoundary, boundaryErr := pullRequests.ChangedSince(ctx, []string{"acme/api"}, time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC))
	afterBoundary, afterErr := pullRequests.ChangedSince(ctx, []string{"acme/api"}, time.Date(2026, 10, 28, 10, 30, 0, 0, time.UTC))

	require.NoError(t, boundaryErr)
	require.NoError(t, afterErr)
	assert.Equal(t, []int{1}, pullRequestNumbers(atBoundary), "12:00+02:00 is stored as 10:00Z")
	assert.Empty(t, afterBoundary)
}

func TestMigration00011_DownDropsFeedTables(t *testing.T) {
	db := persistence.NewTestDB(t)
	ctx := context.Background()
	const countSQL = "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('feed_subscriptions','feed_pull_requests','feed_deliveries')"
	var tableCount int

	require.NoError(t, persistence.MigrateDown(ctx, db))
	require.NoError(t, db.Raw(countSQL).Scan(&tableCount).Error)
	assert.Zero(t, tableCount)

	require.NoError(t, persistence.MigrateUp(ctx, db))
	require.NoError(t, db.Raw(countSQL).Scan(&tableCount).Error)
	assert.Equal(t, 3, tableCount)
}

func TestFeedPullRequests_Repositories(t *testing.T) {
	pullRequests := persistence.NewFeedPullRequests(newFeedDB(t))
	upsertAll(t, pullRequests,
		feedPullRequest("acme/web", 1, accessBaseTime),
		feedPullRequest("acme/api", 1, accessBaseTime),
		feedPullRequest("acme/api", 2, accessBaseTime),
	)

	repositories, err := pullRequests.Repositories(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"acme/api", "acme/web"}, repositories)
}

func TestFeedDeliveries_ClaimReturnsOnlyUndelivered(t *testing.T) {
	db := newFeedDB(t)
	ctx := context.Background()
	deliveries := persistence.NewFeedDeliveries(db)
	upsertAll(t, persistence.NewFeedPullRequests(db),
		feedPullRequest("acme/api", 1, accessBaseTime.Add(time.Minute)),
		feedPullRequest("acme/api", 2, accessBaseTime),
		feedPullRequest("acme/web", 3, accessBaseTime),
	)
	claim := func(tokenID string, repositories ...string) []persistence.FeedPullRequest {
		claimed, err := deliveries.Claim(ctx, persistence.FeedClaim{TokenID: tokenID, Repositories: repositories, DeliveredAt: accessBaseTime})
		require.NoError(t, err)
		return claimed
	}

	first := claim("t1", "acme/api")
	again := claim("t1", "acme/api")
	otherToken := claim("t2", "acme/api")
	widened := claim("t1", "acme/api", "acme/web")

	assert.Equal(t, []int{2, 1}, pullRequestNumbers(first), "ordered by updated_at, not by number")
	assert.Empty(t, again)
	assert.Equal(t, []int{2, 1}, pullRequestNumbers(otherToken))
	assert.Equal(t, []int{3}, pullRequestNumbers(widened))
}

func TestFeedDeliveries_ClaimConcurrentCallersShareNothing(t *testing.T) {
	db := newFeedDB(t)
	ctx := context.Background()
	pullRequests := persistence.NewFeedPullRequests(db)
	deliveries := persistence.NewFeedDeliveries(db)
	for number := 1; number <= 20; number++ {
		upsertAll(t, pullRequests, feedPullRequest("acme/api", number, accessBaseTime))
	}
	const callers = 8
	type outcome struct {
		claimed []persistence.FeedPullRequest
		err     error
	}
	outcomes := make([]outcome, callers)
	start := make(chan struct{})
	var group sync.WaitGroup
	for caller := range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			claimed, err := deliveries.Claim(ctx, persistence.FeedClaim{
				TokenID: "t1", Repositories: []string{"acme/api"}, DeliveredAt: accessBaseTime,
			})
			outcomes[caller] = outcome{claimed: claimed, err: err}
		}()
	}

	close(start)
	group.Wait()

	seen := map[string]bool{}
	for _, result := range outcomes {
		require.NoError(t, result.err)
		for _, item := range result.claimed {
			key := fmt.Sprintf("%s#%d", item.Repository, item.Number)
			assert.False(t, seen[key], "%s was delivered twice", key)
			seen[key] = true
		}
	}
	assert.Len(t, seen, 20)
}

func TestFeedDeliveries_ClaimWithoutRepositories(t *testing.T) {
	deliveries := persistence.NewFeedDeliveries(newFeedDB(t))

	claimed, err := deliveries.Claim(context.Background(), persistence.FeedClaim{TokenID: "t1", DeliveredAt: accessBaseTime})

	require.NoError(t, err)
	assert.Empty(t, claimed)
}
