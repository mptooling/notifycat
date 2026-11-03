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
	addToken(t, db, "t1")
	return db
}

func addToken(t *testing.T, db *gorm.DB, tokenID string) {
	t.Helper()
	invites := persistence.NewAccessInvites(db)
	invite := persistence.AccessInvite{
		ID: "invite-" + tokenID, User: "user-" + tokenID, SecretHash: "invite-hash-" + tokenID,
		ExpiresAt: repoBaseTime.Add(time.Hour), CreatedAt: repoBaseTime,
	}
	require.NoError(t, invites.Create(context.Background(), invite))
	token := persistence.AccessToken{ID: tokenID, User: "user-" + tokenID, SecretHash: "token-hash-" + tokenID, CreatedAt: repoBaseTime}
	require.NoError(t, invites.Redeem(context.Background(), "invite-"+tokenID, token))
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
	got, err := repo.ChangedSince(ctx, domain.SinceQuery{Repositories: []string{"acme/api"}, Since: repoBaseTime.Add(-time.Minute)})

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
	got, err := repo.ChangedSince(ctx, domain.SinceQuery{Repositories: []string{"acme/api"}, Since: repoBaseTime.Add(-time.Minute)})

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

func TestSubscriptionRepo_AddThenListReturnsTokenSubscriptions(t *testing.T) {
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
	assert.ElementsMatch(t, []string{"acme/api", "acme/web"}, []string{listed[0].Repository, listed[1].Repository})
}

func TestSubscriptionRepo_DeleteReturnsTheRemovedSubscription(t *testing.T) {
	repo := infrastructure.NewSubscriptionRepo(persistence.NewFeedSubscriptions(newFeedDB(t)))
	ctx := context.Background()
	_, addErr := repo.Add(ctx, domain.NewSubscriptions{
		TokenID: "t1", Repositories: []string{"acme/api"}, CreatedAt: repoBaseTime,
	})
	require.NoError(t, addErr)
	listed, listErr := repo.List(ctx, "t1")
	require.NoError(t, listErr)
	require.Len(t, listed, 1)

	removed, err := repo.Delete(ctx, "t1", listed[0].ID)

	require.NoError(t, err)
	assert.Equal(t, listed[0], removed)
}

func TestSubscriptionRepo_DeleteUnknownIsErrSubscriptionNotFound(t *testing.T) {
	repo := infrastructure.NewSubscriptionRepo(persistence.NewFeedSubscriptions(newFeedDB(t)))

	_, err := repo.Delete(context.Background(), "t1", 999)

	assert.ErrorIs(t, err, domain.ErrSubscriptionNotFound)
}

func TestDeliveryRepo_ClaimReturnsDomainPullRequestsOnce(t *testing.T) {
	db := newFeedDB(t)
	pullRequests := infrastructure.NewPullRequestRepo(persistence.NewFeedPullRequests(db))
	deliveries := infrastructure.NewDeliveryRepo(persistence.NewFeedDeliveries(db), &recordingTurnPolicy{})
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

func partyPullRequest(number int, updatedAt time.Time, reviewers int) domain.PullRequest {
	pullRequest := fullPullRequest()
	pullRequest.Number = number
	pullRequest.UpdatedAt = updatedAt
	pullRequest.Party = domain.PartyState{
		Reviewers:  reviewers,
		RecordedAt: updatedAt.Add(-time.Hour),
		DeadlineAt: updatedAt.Add(time.Hour),
	}
	return pullRequest
}

func TestSubscriptionRepo_TypeRoundTrips(t *testing.T) {
	repo := infrastructure.NewSubscriptionRepo(persistence.NewFeedSubscriptions(newFeedDB(t)))
	ctx := context.Background()

	_, addErr := repo.Add(ctx, domain.NewSubscriptions{
		TokenID: "t1", Repositories: []string{"acme/api"}, Type: domain.SubscriptionTypeParty, CreatedAt: repoBaseTime,
	})
	listed, listErr := repo.List(ctx, "t1")

	require.NoError(t, addErr)
	require.NoError(t, listErr)
	require.Len(t, listed, 1)
	assert.Equal(t, domain.SubscriptionTypeParty, listed[0].Type)
}

func newSoloSubscriptions(t *testing.T) (*infrastructure.SubscriptionRepo, context.Context) {
	t.Helper()
	repo := infrastructure.NewSubscriptionRepo(persistence.NewFeedSubscriptions(newFeedDB(t)))
	ctx := context.Background()
	_, err := repo.Add(ctx, domain.NewSubscriptions{
		TokenID: "t1", Repositories: []string{"acme/api", "acme/web"}, Type: domain.SubscriptionTypeSolo, CreatedAt: repoBaseTime,
	})
	require.NoError(t, err)
	return repo, ctx
}

func TestSubscriptionRepo_SetTypeChangesTheNamedRepository(t *testing.T) {
	repo, ctx := newSoloSubscriptions(t)

	changed, err := repo.SetType(ctx, "t1", domain.TypeChange{Repositories: []string{"acme/api"}, Type: domain.SubscriptionTypeParty})
	listed, listErr := repo.List(ctx, "t1")

	require.NoError(t, err)
	require.NoError(t, listErr)
	require.Len(t, listed, 2)
	assert.Equal(t, 1, changed)
	typesByRepository := map[string]domain.SubscriptionType{}
	for _, subscription := range listed {
		typesByRepository[subscription.Repository] = subscription.Type
	}
	assert.Equal(t, map[string]domain.SubscriptionType{
		"acme/api": domain.SubscriptionTypeParty,
		"acme/web": domain.SubscriptionTypeSolo,
	}, typesByRepository)
}

func TestSubscriptionRepo_SetTypeCountsNothingWhenTheTypeIsAlreadySet(t *testing.T) {
	repo, ctx := newSoloSubscriptions(t)

	changed, err := repo.SetType(ctx, "t1", domain.TypeChange{Repositories: []string{"acme/api"}, Type: domain.SubscriptionTypeSolo})

	require.NoError(t, err)
	assert.Equal(t, 0, changed, "a subscription that already has the type is not counted")
}

func TestSubscriptionRepo_SetTypeWithNilRepositoriesChangesEverySubscription(t *testing.T) {
	repo, ctx := newSoloSubscriptions(t)

	changed, err := repo.SetType(ctx, "t1", domain.TypeChange{Type: domain.SubscriptionTypeParty})
	listed, listErr := repo.List(ctx, "t1")

	require.NoError(t, err)
	require.NoError(t, listErr)
	require.Len(t, listed, 2)
	assert.Equal(t, 2, changed)
	for _, subscription := range listed {
		assert.Equal(t, domain.SubscriptionTypeParty, subscription.Type)
	}
}

func TestPullRequestRepo_PartyStateRoundTrips(t *testing.T) {
	repo := infrastructure.NewPullRequestRepo(persistence.NewFeedPullRequests(newFeedDB(t)))
	ctx := context.Background()
	want := partyPullRequest(7, repoBaseTime, 3)

	require.NoError(t, repo.Upsert(ctx, want))
	found, isFound, findErr := repo.Find(ctx, domain.PullRequestKey{Repository: "acme/api", Number: 7})
	changed, changedErr := repo.ChangedSince(ctx, domain.SinceQuery{Repositories: []string{"acme/api"}, Since: repoBaseTime.Add(-time.Minute)})

	require.NoError(t, findErr)
	require.NoError(t, changedErr)
	require.True(t, isFound)
	require.Len(t, changed, 1)
	for _, got := range []domain.PullRequest{found, changed[0]} {
		assert.Equal(t, 3, got.Party.Reviewers)
		assert.WithinDuration(t, want.Party.RecordedAt, got.Party.RecordedAt, 0)
		assert.WithinDuration(t, want.Party.DeadlineAt, got.Party.DeadlineAt, 0)
	}
}

func TestPullRequestRepo_ZeroPartyTimesAreNull(t *testing.T) {
	db := newFeedDB(t)
	repo := infrastructure.NewPullRequestRepo(persistence.NewFeedPullRequests(db))
	ctx := context.Background()
	pullRequest := partyPullRequest(7, repoBaseTime, 2)
	pullRequest.Party.DeadlineAt = time.Time{}
	pullRequest.Party.RecordedAt = time.Time{}

	require.NoError(t, repo.Upsert(ctx, pullRequest))
	found, isFound, findErr := repo.Find(ctx, domain.PullRequestKey{Repository: "acme/api", Number: 7})
	var row persistence.FeedPullRequest
	rowErr := db.Take(&row).Error

	require.NoError(t, findErr)
	require.NoError(t, rowErr)
	require.True(t, isFound)
	assert.Nil(t, row.PartyDeadlineAt, "a zero deadline is stored as NULL")
	assert.Nil(t, row.RecordedAt, "a zero record time is stored as NULL")
	assert.Zero(t, found.Party.DeadlineAt)
	assert.Zero(t, found.Party.RecordedAt)
	assert.Equal(t, 2, found.Party.Reviewers)
}

func TestPullRequestRepo_ZeroReviewersStoreThePersistenceDefault(t *testing.T) {
	repo := infrastructure.NewPullRequestRepo(persistence.NewFeedPullRequests(newFeedDB(t)))
	ctx := context.Background()

	require.NoError(t, repo.Upsert(ctx, fullPullRequest()))
	found, _, err := repo.Find(ctx, domain.PullRequestKey{Repository: "acme/api", Number: 7})

	require.NoError(t, err)
	assert.Equal(t, 1, found.Party.Reviewers)
}

func TestPullRequestRepo_FindMissingIsNotFound(t *testing.T) {
	repo := infrastructure.NewPullRequestRepo(persistence.NewFeedPullRequests(newFeedDB(t)))

	_, found, err := repo.Find(context.Background(), domain.PullRequestKey{Repository: "acme/api", Number: 99})

	require.NoError(t, err)
	assert.False(t, found)
}

func TestPullRequestRepo_ReceivedSince(t *testing.T) {
	db := newFeedDB(t)
	pullRequests := infrastructure.NewPullRequestRepo(persistence.NewFeedPullRequests(db))
	deliveries := infrastructure.NewDeliveryRepo(persistence.NewFeedDeliveries(db), &recordingTurnPolicy{})
	ctx := context.Background()
	require.NoError(t, pullRequests.Upsert(ctx, partyPullRequest(7, repoBaseTime, 1)))
	require.NoError(t, pullRequests.Upsert(ctx, partyPullRequest(8, repoBaseTime.Add(time.Minute), 1)))
	_, claimErr := deliveries.Claim(ctx, domain.Claim{TokenID: "t1", Repositories: []string{"acme/api"}, DeliveredAt: repoBaseTime})
	require.NoError(t, claimErr)
	require.NoError(t, pullRequests.Upsert(ctx, partyPullRequest(9, repoBaseTime.Add(2*time.Minute), 1)))
	require.NoError(t, pullRequests.Upsert(ctx, partyPullRequest(10, repoBaseTime.Add(3*time.Minute), 1)))
	require.NoError(t, db.Create(&persistence.FeedDelivery{
		TokenID: "t1", Repository: "acme/api", Number: 10, DeliveredAt: repoBaseTime, Party: true,
	}).Error)

	received, err := pullRequests.ReceivedSince(ctx, domain.SinceQuery{
		TokenID: "t1", Repositories: []string{"acme/api"}, Since: repoBaseTime.Add(time.Minute),
	})

	require.NoError(t, err)
	require.Len(t, received, 2)
	assert.Equal(t, 8, received[0].Number, "a solo delivery counts as received")
	assert.Equal(t, 10, received[1].Number, "a party delivery counts as received")
	assert.Equal(t, 1, received[1].Party.Reviewers)
}

type recordingTurnPolicy struct {
	round domain.PartyRound
	calls int
}

func (policy *recordingTurnPolicy) Choose(round domain.PartyRound) domain.PartyDecision {
	policy.round = round
	policy.calls++
	if len(round.PullRequests) == 0 {
		return domain.PartyDecision{}
	}
	return domain.PartyDecision{Take: []domain.PullRequestKey{round.PullRequests[0].Key}, Taken: 4, Waiting: 5}
}

func TestDeliveryRepo_ClaimPartyBuildsTheRound(t *testing.T) {
	db := newFeedDB(t)
	for _, memberID := range []string{"t2", "t3", "t4"} {
		addToken(t, db, memberID)
	}
	ctx := context.Background()
	subscriptions := infrastructure.NewSubscriptionRepo(persistence.NewFeedSubscriptions(db))
	pullRequests := infrastructure.NewPullRequestRepo(persistence.NewFeedPullRequests(db))
	polls := persistence.NewFeedPolls(db)
	policy := &recordingTurnPolicy{}
	deliveries := infrastructure.NewDeliveryRepo(persistence.NewFeedDeliveries(db), policy)
	for _, memberID := range []string{"t1", "t2", "t3", "t4"} {
		_, addErr := subscriptions.Add(ctx, domain.NewSubscriptions{
			TokenID: memberID, Repositories: []string{"acme/api"}, Type: domain.SubscriptionTypeParty, CreatedAt: repoBaseTime,
		})
		require.NoError(t, addErr)
		require.NoError(t, polls.Record(ctx, persistence.FeedPollRecord{TokenID: memberID, PolledAt: repoBaseTime}))
	}
	firstPullRequest := partyPullRequest(7, repoBaseTime, 2)
	secondPullRequest := partyPullRequest(8, repoBaseTime.Add(time.Minute), 1)
	require.NoError(t, pullRequests.Upsert(ctx, firstPullRequest))
	require.NoError(t, pullRequests.Upsert(ctx, secondPullRequest))
	require.NoError(t, db.Create(&persistence.FeedDelivery{
		TokenID: "t2", Repository: "acme/api", Number: 7, DeliveredAt: repoBaseTime, Party: true,
	}).Error)
	require.NoError(t, db.Create(&persistence.FeedDelivery{
		TokenID: "t3", Repository: "acme/api", Number: 7, DeliveredAt: repoBaseTime, Party: false,
	}).Error)
	require.NoError(t, db.Create(&persistence.FeedPartyLoad{TokenID: "t2", Repository: "acme/api", Day: "2026-10-28", Taken: 3}).Error)
	deliveredAt := repoBaseTime.Add(time.Hour)

	result, err := deliveries.ClaimParty(ctx, domain.PartyClaim{
		TokenID: "t1", Repositories: []string{"acme/api"}, DeliveredAt: deliveredAt,
		Day: "2026-10-28", ActiveSince: repoBaseTime.Add(-time.Hour),
	})

	require.NoError(t, err)
	round := policy.round
	require.Len(t, round.PullRequests, 2)
	assert.Equal(t, "t1", round.TokenID)
	assert.WithinDuration(t, deliveredAt, round.Now, 0)
	assert.Equal(t, domain.PullRequestKey{Repository: "acme/api", Number: 7}, round.PullRequests[0].Key)
	assert.Equal(t, domain.PullRequestKey{Repository: "acme/api", Number: 8}, round.PullRequests[1].Key)
	assert.Equal(t, 2, round.PullRequests[0].Party.Reviewers)
	assert.WithinDuration(t, firstPullRequest.Party.DeadlineAt, round.PullRequests[0].Party.DeadlineAt, 0)
	assert.Equal(t, 1, round.PullRequests[0].PartyDeliveries, "a solo delivery is not a party delivery")
	assert.Equal(t, []string{"t4"}, round.PullRequests[0].Candidates, "members with a delivery of the pull request are not candidates")
	assert.Equal(t, 0, round.PullRequests[1].PartyDeliveries)
	assert.Equal(t, []string{"t2", "t3", "t4"}, round.PullRequests[1].Candidates)
	assert.Equal(t, map[domain.PartyMember]int{{TokenID: "t2", Repository: "acme/api"}: 3}, round.Loads)
	require.Len(t, result.Delivered, 1)
	assert.Equal(t, 7, result.Delivered[0].Number)
	assert.Equal(t, 4, result.Taken)
	assert.Equal(t, 5, result.Waiting)
}

func TestDeliveryRepo_ClaimPartyCountsCappedPullRequestsAsTaken(t *testing.T) {
	db := newFeedDB(t)
	addToken(t, db, "t2")
	ctx := context.Background()
	pullRequests := infrastructure.NewPullRequestRepo(persistence.NewFeedPullRequests(db))
	policy := &recordingTurnPolicy{}
	deliveries := infrastructure.NewDeliveryRepo(persistence.NewFeedDeliveries(db), policy)
	require.NoError(t, pullRequests.Upsert(ctx, partyPullRequest(7, repoBaseTime, 1)))
	require.NoError(t, pullRequests.Upsert(ctx, partyPullRequest(8, repoBaseTime, 1)))
	require.NoError(t, db.Create(&persistence.FeedDelivery{
		TokenID: "t2", Repository: "acme/api", Number: 7, DeliveredAt: repoBaseTime, Party: true,
	}).Error)
	require.NoError(t, db.Create(&persistence.FeedDelivery{
		TokenID: "t1", Repository: "acme/api", Number: 8, DeliveredAt: repoBaseTime,
	}).Error)

	result, err := deliveries.ClaimParty(ctx, domain.PartyClaim{
		TokenID: "t1", Repositories: []string{"acme/api"}, DeliveredAt: repoBaseTime.Add(time.Hour),
		Day: "2026-10-28", ActiveSince: repoBaseTime.Add(-time.Hour),
	})

	require.NoError(t, err)
	assert.Empty(t, result.Delivered, "a PR at its cap is never delivered, whatever the policy picks")
	assert.Equal(t, 1, result.Taken, "a PR the token already received is not taken by others")
	assert.Zero(t, result.Waiting)
	assert.Zero(t, policy.calls, "the policy is not asked without a PR below its cap")
}

func TestDeliveryRepo_ClaimPartyWithoutRepositoriesSkipsThePolicy(t *testing.T) {
	policy := &recordingTurnPolicy{}
	deliveries := infrastructure.NewDeliveryRepo(persistence.NewFeedDeliveries(newFeedDB(t)), policy)

	result, err := deliveries.ClaimParty(context.Background(), domain.PartyClaim{TokenID: "t1", DeliveredAt: repoBaseTime})

	require.NoError(t, err)
	assert.Empty(t, result.Delivered)
	assert.Zero(t, result.Taken)
	assert.Zero(t, result.Waiting)
	assert.Zero(t, policy.calls, "the policy is not asked without repositories")
}

func TestPollRepo_RecordWritesAtMostOncePerResolution(t *testing.T) {
	testCases := []struct {
		name         string
		earlierPolls []time.Time
		polledAt     time.Time
		want         time.Time
	}{
		{
			name:     "the first poll inserts",
			polledAt: repoBaseTime,
			want:     repoBaseTime,
		},
		{
			name:         "a poll within the resolution keeps the stored time",
			earlierPolls: []time.Time{repoBaseTime},
			polledAt:     repoBaseTime.Add(domain.PollResolution - time.Second),
			want:         repoBaseTime,
		},
		{
			name:         "a poll after the resolution replaces the stored time",
			earlierPolls: []time.Time{repoBaseTime},
			polledAt:     repoBaseTime.Add(domain.PollResolution + time.Second),
			want:         repoBaseTime.Add(domain.PollResolution + time.Second),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := newFeedDB(t)
			repo := infrastructure.NewPollRepo(persistence.NewFeedPolls(db))
			ctx := context.Background()
			for _, earlierPoll := range testCase.earlierPolls {
				require.NoError(t, repo.Record(ctx, "t1", earlierPoll))
			}

			err := repo.Record(ctx, "t1", testCase.polledAt)

			require.NoError(t, err)
			var row persistence.FeedPoll
			require.NoError(t, db.Where("token_id = ?", "t1").Take(&row).Error)
			assert.WithinDuration(t, testCase.want, row.PolledAt, 0)
		})
	}
}

func TestConfigPartySettings_ReturnsTheGlobalSettings(t *testing.T) {
	settings := domain.Settings{Party: domain.PartySettings{Reviewers: 2, Deadline: 90 * time.Minute}}
	resolver := infrastructure.NewConfigPartySettings(settings)

	got, err := resolver.Resolve(context.Background(), domain.PullRequestKey{Repository: "acme/api", Number: 7})

	require.NoError(t, err)
	assert.Equal(t, settings.Party, got)
}
