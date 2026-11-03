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

func TestFeedSubscriptions_AddKeepsExistingType(t *testing.T) {
	subscriptions := persistence.NewFeedSubscriptions(newFeedDB(t))
	ctx := context.Background()
	_, err := subscriptions.Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID: "t1", Repositories: []string{"acme/api"}, CreatedAt: accessBaseTime,
	})
	require.NoError(t, err)

	added, err := subscriptions.Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID: "t1", Repositories: []string{"acme/api"}, CreatedAt: accessBaseTime, Type: "party",
	})

	require.NoError(t, err)
	assert.Zero(t, added)
	assert.Equal(t, "solo", subscriptionTypes(t, subscriptions, "t1")["acme/api"], "an existing row keeps its type")
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
	require.NoError(t, persistence.NewFeedPolls(db).Record(ctx, "t1", accessBaseTime))
	require.NoError(t, db.Create(&persistence.FeedPartyLoad{TokenID: "t1", Repository: "acme/api", Day: "2026-10-28", Taken: 1}).Error)
	require.EqualValues(t, 1, countTable(t, db, "feed_polls", "token_id = ?", "t1"))
	require.EqualValues(t, 1, countTable(t, db, "feed_party_loads", "token_id = ?", "t1"))

	require.NoError(t, persistence.NewAccessTokens(db).Delete(ctx, "t1"))

	assert.Zero(t, countTable(t, db, "feed_subscriptions", "token_id = ?", "t1"))
	assert.Zero(t, countTable(t, db, "feed_deliveries", "token_id = ?", "t1"))
	assert.Zero(t, countTable(t, db, "feed_polls", "token_id = ?", "t1"))
	assert.Zero(t, countTable(t, db, "feed_party_loads", "token_id = ?", "t1"))
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

	require.NoError(t, persistence.MigrateDown(ctx, db)) // 00012
	require.NoError(t, persistence.MigrateDown(ctx, db)) // 00011
	require.NoError(t, db.Raw(countSQL).Scan(&tableCount).Error)
	assert.Zero(t, tableCount)

	require.NoError(t, persistence.MigrateUp(ctx, db))
	require.NoError(t, db.Raw(countSQL).Scan(&tableCount).Error)
	assert.Equal(t, 3, tableCount)
}

func TestMigration00012_DownRemovesPartyTablesAndColumns(t *testing.T) {
	db := persistence.NewTestDB(t)
	ctx := context.Background()
	const tablesSQL = "SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('feed_polls','feed_party_loads')"
	const typeColumnSQL = "SELECT count(*) FROM pragma_table_info('feed_subscriptions') WHERE name = 'type'"
	var tableCount, typeColumnCount int

	require.NoError(t, persistence.MigrateDown(ctx, db))
	require.NoError(t, db.Raw(tablesSQL).Scan(&tableCount).Error)
	require.NoError(t, db.Raw(typeColumnSQL).Scan(&typeColumnCount).Error)
	assert.Zero(t, tableCount)
	assert.Zero(t, typeColumnCount)

	require.NoError(t, persistence.MigrateUp(ctx, db))
	require.NoError(t, db.Raw(tablesSQL).Scan(&tableCount).Error)
	require.NoError(t, db.Raw(typeColumnSQL).Scan(&typeColumnCount).Error)
	assert.Equal(t, 2, tableCount)
	assert.Equal(t, 1, typeColumnCount)
}

func TestFeedSubscriptions_AddStoresTypeAndDefaultsToSolo(t *testing.T) {
	subscriptions := persistence.NewFeedSubscriptions(newFeedDB(t))
	ctx := context.Background()
	_, err := subscriptions.Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID: "t1", Repositories: []string{"acme/api"}, CreatedAt: accessBaseTime, Type: "party",
	})
	require.NoError(t, err)
	_, err = subscriptions.Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID: "t1", Repositories: []string{"acme/web"}, CreatedAt: accessBaseTime,
	})
	require.NoError(t, err)

	listed, err := subscriptions.List(ctx, "t1")

	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, "party", listed[0].Type)
	assert.Equal(t, "solo", listed[1].Type)
}

func seedSoloSubscriptions(t *testing.T, subscriptions *persistence.FeedSubscriptions) {
	t.Helper()
	ctx := context.Background()
	_, err := subscriptions.Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID: "t1", Repositories: []string{"acme/api", "acme/web", "*"}, CreatedAt: accessBaseTime,
	})
	require.NoError(t, err)
	_, err = subscriptions.Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID: "t2", Repositories: []string{"acme/api"}, CreatedAt: accessBaseTime,
	})
	require.NoError(t, err)
}

func subscriptionTypes(t *testing.T, subscriptions *persistence.FeedSubscriptions, tokenID string) map[string]string {
	t.Helper()
	listed, err := subscriptions.List(context.Background(), tokenID)
	require.NoError(t, err)
	types := make(map[string]string, len(listed))
	for _, subscription := range listed {
		types[subscription.Repository] = subscription.Type
	}
	return types
}

func TestFeedSubscriptions_SetTypeChangesOnlyTheNamedRows(t *testing.T) {
	subscriptions := persistence.NewFeedSubscriptions(newFeedDB(t))
	seedSoloSubscriptions(t, subscriptions)

	changed, err := subscriptions.SetType(context.Background(), persistence.FeedTypeChange{
		TokenID: "t1", Repositories: []string{"acme/api", "*"}, Type: "party",
	})

	require.NoError(t, err)
	assert.Equal(t, 2, changed)
	assert.Equal(t, map[string]string{"acme/api": "party", "acme/web": "solo", "*": "party"}, subscriptionTypes(t, subscriptions, "t1"))
	assert.Equal(t, map[string]string{"acme/api": "solo"}, subscriptionTypes(t, subscriptions, "t2"))
}

func TestFeedSubscriptions_SetTypeCountsOnlyChangedRows(t *testing.T) {
	subscriptions := persistence.NewFeedSubscriptions(newFeedDB(t))
	seedSoloSubscriptions(t, subscriptions)
	change := persistence.FeedTypeChange{TokenID: "t1", Type: "party"}
	_, err := subscriptions.SetType(context.Background(), change)
	require.NoError(t, err)

	changed, err := subscriptions.SetType(context.Background(), change)

	require.NoError(t, err)
	assert.Zero(t, changed)
}

func TestFeedSubscriptions_SetTypeWithoutRepositoriesChangesEveryRow(t *testing.T) {
	subscriptions := persistence.NewFeedSubscriptions(newFeedDB(t))
	seedSoloSubscriptions(t, subscriptions)

	changed, err := subscriptions.SetType(context.Background(), persistence.FeedTypeChange{TokenID: "t1", Type: "party"})

	require.NoError(t, err)
	assert.Equal(t, 3, changed)
	assert.Equal(t, map[string]string{"acme/api": "party", "acme/web": "party", "*": "party"}, subscriptionTypes(t, subscriptions, "t1"))
	assert.Equal(t, map[string]string{"acme/api": "solo"}, subscriptionTypes(t, subscriptions, "t2"))
}

func TestFeedPolls_RecordUpserts(t *testing.T) {
	db := newFeedDB(t)
	polls := persistence.NewFeedPolls(db)
	ctx := context.Background()
	later := accessBaseTime.Add(time.Hour)

	require.NoError(t, polls.Record(ctx, "t1", accessBaseTime))
	require.NoError(t, polls.Record(ctx, "t1", later))

	var stored []persistence.FeedPoll
	require.NoError(t, db.Find(&stored).Error)
	require.Len(t, stored, 1)
	assert.Equal(t, "t1", stored[0].TokenID)
	assert.WithinDuration(t, later, stored[0].PolledAt, 0)
}

func TestFeedPolls_RecordStoresNonUTCTimesInUTC(t *testing.T) {
	db := newFeedDB(t)
	polls := persistence.NewFeedPolls(db)
	offset := time.FixedZone("plus2", 2*60*60)

	require.NoError(t, polls.Record(context.Background(), "t1", time.Date(2026, 10, 28, 12, 0, 0, 0, offset)))

	assert.EqualValues(t, 1, countTable(t, db, "feed_polls", "polled_at = ?", time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)),
		"12:00+02:00 is stored as 10:00Z")
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

func TestFeedPullRequests_UpsertKeepsTheFirstRecordedAt(t *testing.T) {
	pullRequests := persistence.NewFeedPullRequests(newFeedDB(t))
	ctx := context.Background()
	firstRecordedAt := accessBaseTime
	laterRecordedAt := accessBaseTime.Add(time.Hour)
	first := feedPullRequest("acme/api", 1, accessBaseTime)
	first.RecordedAt = &firstRecordedAt
	second := feedPullRequest("acme/api", 1, accessBaseTime.Add(time.Minute))
	second.RecordedAt = &laterRecordedAt
	upsertAll(t, pullRequests, first, second)

	stored, err := pullRequests.Find(ctx, "acme/api", 1)

	require.NoError(t, err)
	require.NotNil(t, stored.RecordedAt)
	assert.WithinDuration(t, firstRecordedAt, *stored.RecordedAt, 0, "the first record time stays")
}

func TestFeedPullRequests_UpsertReplacesPartySettings(t *testing.T) {
	db := newFeedDB(t)
	pullRequests := persistence.NewFeedPullRequests(db)
	ctx := context.Background()
	offset := time.FixedZone("plus2", 2*60*60)
	firstDeadline := accessBaseTime.Add(time.Hour)
	secondDeadline := time.Date(2026, 10, 28, 17, 0, 0, 0, offset)
	recordedAt := time.Date(2026, 10, 28, 14, 0, 0, 0, offset)
	first := feedPullRequest("acme/api", 1, accessBaseTime)
	first.PartyReviewers = 1
	first.RecordedAt = &recordedAt
	first.PartyDeadlineAt = &firstDeadline
	second := feedPullRequest("acme/api", 1, accessBaseTime.Add(time.Minute))
	second.PartyReviewers = 3
	second.PartyDeadlineAt = &secondDeadline
	upsertAll(t, pullRequests, first, second)

	stored, err := pullRequests.Find(ctx, "acme/api", 1)

	require.NoError(t, err)
	require.NotNil(t, stored.PartyDeadlineAt)
	assert.Equal(t, 3, stored.PartyReviewers)
	assert.WithinDuration(t, secondDeadline, *stored.PartyDeadlineAt, 0)
	assert.EqualValues(t, 1, countTable(t, db, "feed_pull_requests", "party_deadline_at = ?",
		time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC)), "17:00+02:00 is stored as 15:00Z")
	assert.EqualValues(t, 1, countTable(t, db, "feed_pull_requests", "recorded_at = ?",
		time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)), "14:00+02:00 is stored as 12:00Z")
}

func TestFeedPullRequests_FindMissingIsErrNotFound(t *testing.T) {
	pullRequests := persistence.NewFeedPullRequests(newFeedDB(t))

	_, err := pullRequests.Find(context.Background(), "acme/api", 1)

	require.ErrorIs(t, err, persistence.ErrNotFound)
}

func TestFeedPullRequests_ReceivedSince(t *testing.T) {
	db := newFeedDB(t)
	pullRequests := persistence.NewFeedPullRequests(db)
	ctx := context.Background()
	since := accessBaseTime
	upsertAll(t, pullRequests,
		feedPullRequest("acme/api", 1, since.Add(time.Minute)),
		feedPullRequest("acme/api", 2, since.Add(2*time.Minute)),
		feedPullRequest("acme/api", 3, since.Add(3*time.Minute)),
		feedPullRequest("acme/api", 4, since.Add(-time.Minute)),
	)
	require.NoError(t, db.Create(&persistence.FeedDelivery{
		TokenID: "t1", Repository: "acme/api", Number: 1, DeliveredAt: since,
	}).Error)
	require.NoError(t, db.Create(&persistence.FeedDelivery{
		TokenID: "t1", Repository: "acme/api", Number: 2, DeliveredAt: since, Party: true,
	}).Error)
	require.NoError(t, db.Create(&persistence.FeedDelivery{
		TokenID: "t1", Repository: "acme/api", Number: 4, DeliveredAt: since,
	}).Error)

	received, err := pullRequests.ReceivedSince(ctx, persistence.FeedReceivedQuery{
		TokenID: "t1", Repositories: []string{"acme/api"}, Since: since,
	})
	other, otherErr := pullRequests.ReceivedSince(ctx, persistence.FeedReceivedQuery{
		TokenID: "t2", Repositories: []string{"acme/api"}, Since: since,
	})
	none, noneErr := pullRequests.ReceivedSince(ctx, persistence.FeedReceivedQuery{TokenID: "t1", Since: since})
	offsetSince := time.Date(2026, 10, 28, 14, 0, 0, 0, time.FixedZone("plus2", 2*60*60))
	offsetReceived, offsetErr := pullRequests.ReceivedSince(ctx, persistence.FeedReceivedQuery{
		TokenID: "t1", Repositories: []string{"acme/api"}, Since: offsetSince,
	})

	require.NoError(t, err)
	require.NoError(t, otherErr)
	require.NoError(t, noneErr)
	require.NoError(t, offsetErr)
	assert.Equal(t, []int{1, 2}, pullRequestNumbers(received))
	assert.Equal(t, []int{1, 2}, pullRequestNumbers(offsetReceived), "an offset since is compared in UTC")
	assert.Empty(t, other)
	assert.NotNil(t, none)
	assert.Empty(t, none)
}

var partyBaseTime = time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)

const partyDay = "2026-11-03"

func newPartyClaim(tokenID string, choose func(persistence.FeedPartySnapshot) []persistence.FeedPullRequestKey) persistence.FeedPartyClaim {
	return persistence.FeedPartyClaim{
		TokenID:      tokenID,
		Repositories: []string{"acme/api"},
		DeliveredAt:  partyBaseTime,
		Day:          partyDay,
		ActiveSince:  partyBaseTime,
		Choose:       choose,
	}
}

func captureSnapshot(captured *persistence.FeedPartySnapshot) func(persistence.FeedPartySnapshot) []persistence.FeedPullRequestKey {
	return func(snapshot persistence.FeedPartySnapshot) []persistence.FeedPullRequestKey {
		*captured = snapshot
		return nil
	}
}

func takeNothing(persistence.FeedPartySnapshot) []persistence.FeedPullRequestKey {
	return nil
}

func takeFirstWaiting(snapshot persistence.FeedPartySnapshot) []persistence.FeedPullRequestKey {
	if len(snapshot.Waiting) == 0 {
		return nil
	}
	first := snapshot.Waiting[0]
	return []persistence.FeedPullRequestKey{{Repository: first.Repository, Number: first.Number}}
}

func takeBelowTheCap(snapshot persistence.FeedPartySnapshot) []persistence.FeedPullRequestKey {
	partyDeliveries := map[persistence.FeedPullRequestKey]int{}
	for _, delivery := range snapshot.Deliveries {
		if delivery.Party {
			partyDeliveries[persistence.FeedPullRequestKey{Repository: delivery.Repository, Number: delivery.Number}]++
		}
	}
	taken := []persistence.FeedPullRequestKey{}
	for _, pullRequest := range snapshot.Waiting {
		key := persistence.FeedPullRequestKey{Repository: pullRequest.Repository, Number: pullRequest.Number}
		if partyDeliveries[key] < pullRequest.PartyReviewers {
			taken = append(taken, key)
		}
	}
	return taken
}

func subscribe(t *testing.T, db *gorm.DB, tokenID string, subscriptionType string, repositories ...string) {
	t.Helper()
	_, err := persistence.NewFeedSubscriptions(db).Add(context.Background(), persistence.FeedSubscriptionBatch{
		TokenID: tokenID, Repositories: repositories, CreatedAt: accessBaseTime, Type: subscriptionType,
	})
	require.NoError(t, err)
}

func recordPoll(t *testing.T, db *gorm.DB, tokenID string, polledAt time.Time) {
	t.Helper()
	require.NoError(t, persistence.NewFeedPolls(db).Record(context.Background(), tokenID, polledAt))
}

func seedDeliveries(t *testing.T, db *gorm.DB, deliveries ...persistence.FeedDelivery) {
	t.Helper()
	for _, delivery := range deliveries {
		require.NoError(t, db.Create(&delivery).Error)
	}
}

func seedPartyLoads(t *testing.T, db *gorm.DB, loads ...persistence.FeedPartyLoad) {
	t.Helper()
	for _, load := range loads {
		require.NoError(t, db.Create(&load).Error)
	}
}

func partyLoadTaken(t *testing.T, db *gorm.DB, tokenID string) int {
	t.Helper()
	var load persistence.FeedPartyLoad
	require.NoError(t, db.Where("token_id = ? AND repository = ? AND day = ?", tokenID, "acme/api", partyDay).Take(&load).Error)
	return load.Taken
}

func deliveryLabels(deliveries []persistence.FeedDelivery) []string {
	labels := make([]string, 0, len(deliveries))
	for _, delivery := range deliveries {
		labels = append(labels, fmt.Sprintf("%s %s#%d party=%t", delivery.TokenID, delivery.Repository, delivery.Number, delivery.Party))
	}
	return labels
}

func TestFeedDeliveries_ClaimPartySnapshotMembers(t *testing.T) {
	db := newFeedDB(t)
	for _, tokenID := range []string{"t3", "t4", "t5", "t6", "t7"} {
		seedToken(t, db, tokenID, accessBaseTime)
	}
	subscribe(t, db, "t1", "party", "acme/api")
	subscribe(t, db, "t2", "party", "acme/api")
	subscribe(t, db, "t3", "party", "acme/api")
	subscribe(t, db, "t4", "party", "acme/api")
	subscribe(t, db, "t5", "solo", "acme/api")
	subscribe(t, db, "t6", "party", "*")
	subscribe(t, db, "t7", "party", "*")
	subscribe(t, db, "t7", "solo", "acme/api")
	for _, tokenID := range []string{"t1", "t2", "t5", "t6", "t7"} {
		recordPoll(t, db, tokenID, partyBaseTime)
	}
	recordPoll(t, db, "t3", partyBaseTime.Add(-time.Second))
	var snapshot persistence.FeedPartySnapshot
	claim := newPartyClaim("t1", captureSnapshot(&snapshot))
	claim.ActiveSince = partyBaseTime.In(time.FixedZone("plus2", 2*60*60))

	_, err := persistence.NewFeedDeliveries(db).ClaimParty(context.Background(), claim)

	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"acme/api": {"t2", "t6"}}, snapshot.Members,
		"a poll at ActiveSince counts, and an offset ActiveSince is compared in UTC")
}

func TestFeedDeliveries_ClaimPartySnapshotWaiting(t *testing.T) {
	db := newFeedDB(t)
	seedToken(t, db, "t3", accessBaseTime)
	upsertAll(t, persistence.NewFeedPullRequests(db),
		feedPullRequest("acme/api", 1, partyBaseTime),
		feedPullRequest("acme/api", 2, partyBaseTime.Add(time.Minute)),
		feedPullRequest("acme/api", 5, partyBaseTime),
		feedPullRequest("acme/api", 3, partyBaseTime),
		feedPullRequest("acme/web", 4, partyBaseTime),
	)
	seedDeliveries(t, db,
		persistence.FeedDelivery{TokenID: "t1", Repository: "acme/api", Number: 1, DeliveredAt: partyBaseTime},
		persistence.FeedDelivery{TokenID: "t2", Repository: "acme/api", Number: 1, DeliveredAt: partyBaseTime, Party: true},
		persistence.FeedDelivery{TokenID: "t2", Repository: "acme/api", Number: 2, DeliveredAt: partyBaseTime, Party: true},
		persistence.FeedDelivery{TokenID: "t3", Repository: "acme/api", Number: 2, DeliveredAt: partyBaseTime},
	)
	var snapshot persistence.FeedPartySnapshot

	_, err := persistence.NewFeedDeliveries(db).ClaimParty(context.Background(), newPartyClaim("t1", captureSnapshot(&snapshot)))

	require.NoError(t, err)
	assert.Equal(t, []int{3, 5, 2}, pullRequestNumbers(snapshot.Waiting),
		"ordered by updated_at, then number, without the PR the claimant has as a solo delivery")
	assert.ElementsMatch(t, []string{"t2 acme/api#2 party=true", "t3 acme/api#2 party=false"}, deliveryLabels(snapshot.Deliveries),
		"every row of a waiting PR, solo rows too, and no row of a PR that is not waiting")
}

func TestFeedDeliveries_ClaimPartySnapshotLoads(t *testing.T) {
	db := newFeedDB(t)
	seedPartyLoads(t, db,
		persistence.FeedPartyLoad{TokenID: "t1", Repository: "acme/api", Day: partyDay, Taken: 2},
		persistence.FeedPartyLoad{TokenID: "t2", Repository: "acme/api", Day: partyDay, Taken: 1},
		persistence.FeedPartyLoad{TokenID: "t2", Repository: "acme/web", Day: partyDay, Taken: 3},
		persistence.FeedPartyLoad{TokenID: "t2", Repository: "acme/api", Day: "2026-11-04", Taken: 4},
	)
	var snapshot persistence.FeedPartySnapshot

	_, err := persistence.NewFeedDeliveries(db).ClaimParty(context.Background(), newPartyClaim("t1", captureSnapshot(&snapshot)))

	require.NoError(t, err)
	assert.Equal(t, []persistence.FeedPartyLoad{
		{TokenID: "t1", Repository: "acme/api", Day: partyDay, Taken: 2},
		{TokenID: "t2", Repository: "acme/api", Day: partyDay, Taken: 1},
	}, snapshot.Loads)
}

func TestFeedDeliveries_ClaimPartyRecordsDeliveriesAndLoad(t *testing.T) {
	db := newFeedDB(t)
	ctx := context.Background()
	pullRequests := persistence.NewFeedPullRequests(db)
	deliveries := persistence.NewFeedDeliveries(db)
	upsertAll(t, pullRequests,
		feedPullRequest("acme/api", 1, partyBaseTime),
		feedPullRequest("acme/api", 2, partyBaseTime.Add(time.Minute)),
	)
	claim := newPartyClaim("t1", takeFirstWaiting)
	claim.DeliveredAt = partyBaseTime.In(time.FixedZone("plus2", 2*60*60))

	first, firstErr := deliveries.ClaimParty(ctx, claim)
	takenAfterFirst := partyLoadTaken(t, db, "t1")
	second, secondErr := deliveries.ClaimParty(ctx, claim)
	takenAfterSecond := partyLoadTaken(t, db, "t1")
	partyRows := countTable(t, db, "feed_deliveries", "token_id = ? AND party = ? AND delivered_at = ?", "t1", true, partyBaseTime)
	require.NoError(t, pullRequests.Delete(ctx, "acme/api", 1))
	require.NoError(t, pullRequests.Delete(ctx, "acme/api", 2))
	takenAfterDelete := partyLoadTaken(t, db, "t1")

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	assert.Equal(t, []int{1}, pullRequestNumbers(first))
	assert.Equal(t, []int{2}, pullRequestNumbers(second))
	assert.EqualValues(t, 2, partyRows, "party rows store delivered_at in UTC")
	assert.Equal(t, 1, takenAfterFirst)
	assert.Equal(t, 2, takenAfterSecond)
	assert.Equal(t, 2, takenAfterDelete, "the load outlives the pull requests")
}

func TestFeedDeliveries_ClaimPartySkipsDeliveredKeysAndKeepsTheChosenOrder(t *testing.T) {
	db := newFeedDB(t)
	upsertAll(t, persistence.NewFeedPullRequests(db),
		feedPullRequest("acme/api", 1, partyBaseTime),
		feedPullRequest("acme/api", 2, partyBaseTime),
		feedPullRequest("acme/api", 3, partyBaseTime),
	)
	seedDeliveries(t, db, persistence.FeedDelivery{TokenID: "t1", Repository: "acme/api", Number: 3, DeliveredAt: partyBaseTime})
	claim := newPartyClaim("t1", func(persistence.FeedPartySnapshot) []persistence.FeedPullRequestKey {
		return []persistence.FeedPullRequestKey{
			{Repository: "acme/api", Number: 2},
			{Repository: "acme/api", Number: 3},
			{Repository: "acme/api", Number: 1},
			{Repository: "acme/api", Number: 2},
		}
	})

	claimed, err := persistence.NewFeedDeliveries(db).ClaimParty(context.Background(), claim)

	require.NoError(t, err)
	assert.Equal(t, []int{2, 1}, pullRequestNumbers(claimed))
	assert.Equal(t, 2, partyLoadTaken(t, db, "t1"), "only inserted rows add to the load")
	assert.EqualValues(t, 2, countTable(t, db, "feed_deliveries", "token_id = ? AND party = ?", "t1", true),
		"an earlier solo delivery stays solo")
}

func TestFeedDeliveries_ClaimPartyRemovesEarlierDays(t *testing.T) {
	db := newFeedDB(t)
	seedPartyLoads(t, db,
		persistence.FeedPartyLoad{TokenID: "t1", Repository: "acme/api", Day: "2026-11-02", Taken: 1},
		persistence.FeedPartyLoad{TokenID: "t2", Repository: "acme/web", Day: "2026-11-02", Taken: 1},
		persistence.FeedPartyLoad{TokenID: "t1", Repository: "acme/api", Day: partyDay, Taken: 1},
	)

	_, err := persistence.NewFeedDeliveries(db).ClaimParty(context.Background(), newPartyClaim("t1", takeNothing))

	require.NoError(t, err)
	assert.Zero(t, countTable(t, db, "feed_party_loads", "day = ?", "2026-11-02"), "earlier rows of every token and repository go")
	assert.EqualValues(t, 1, countTable(t, db, "feed_party_loads", "day = ?", partyDay))
}

func TestFeedDeliveries_ClaimPartyWithoutRepositories(t *testing.T) {
	chooseCalled := false
	claim := newPartyClaim("t1", func(persistence.FeedPartySnapshot) []persistence.FeedPullRequestKey {
		chooseCalled = true
		return nil
	})
	claim.Repositories = nil

	claimed, err := persistence.NewFeedDeliveries(newFeedDB(t)).ClaimParty(context.Background(), claim)

	require.NoError(t, err)
	assert.NotNil(t, claimed)
	assert.Empty(t, claimed)
	assert.False(t, chooseCalled)
}

func TestFeedDeliveries_ClaimPartyConcurrentMembersNeverPassTheCap(t *testing.T) {
	db := newFeedDB(t)
	ctx := context.Background()
	const members = 8
	tokenIDs := make([]string, 0, members)
	for member := 1; member <= members; member++ {
		tokenID := fmt.Sprintf("t%d", member)
		if member > 2 {
			seedToken(t, db, tokenID, accessBaseTime)
		}
		subscribe(t, db, tokenID, "party", "acme/api")
		recordPoll(t, db, tokenID, partyBaseTime)
		tokenIDs = append(tokenIDs, tokenID)
	}
	capped := feedPullRequest("acme/api", 1, partyBaseTime)
	capped.PartyReviewers = 2
	upsertAll(t, persistence.NewFeedPullRequests(db), capped)
	deliveries := persistence.NewFeedDeliveries(db)
	type outcome struct {
		claimed []persistence.FeedPullRequest
		err     error
	}
	outcomes := make([]outcome, members)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index, tokenID := range tokenIDs {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			claimed, err := deliveries.ClaimParty(ctx, newPartyClaim(tokenID, takeBelowTheCap))
			outcomes[index] = outcome{claimed: claimed, err: err}
		}()
	}

	close(start)
	group.Wait()

	delivered := 0
	for _, result := range outcomes {
		require.NoError(t, result.err)
		delivered += len(result.claimed)
	}
	assert.Equal(t, 2, delivered)
	assert.EqualValues(t, 2, countTable(t, db, "feed_deliveries", "repository = ? AND number = ? AND party = ?", "acme/api", 1, true))
}
