package infrastructure

import (
	"context"
	"fmt"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// DeliveryRepo adapts persistence.FeedDeliveries to domain.DeliveryStore.
type DeliveryRepo struct {
	deliveries *persistence.FeedDeliveries
	policy     domain.TurnPolicy
}

var _ domain.DeliveryStore = (*DeliveryRepo)(nil)

// NewDeliveryRepo wraps the deliveries repository; the policy decides party turns.
func NewDeliveryRepo(deliveries *persistence.FeedDeliveries, policy domain.TurnPolicy) *DeliveryRepo {
	return &DeliveryRepo{deliveries: deliveries, policy: policy}
}

// Claim implements domain.DeliveryStore.
func (repo *DeliveryRepo) Claim(ctx context.Context, claim domain.Claim) ([]domain.PullRequest, error) {
	rows, err := repo.deliveries.Claim(ctx, persistence.FeedClaim{
		TokenID:      claim.TokenID,
		Repositories: claim.Repositories,
		DeliveredAt:  claim.DeliveredAt,
	})
	if err != nil {
		return nil, fmt.Errorf("feed: claim pull requests: %w", err)
	}
	return pullRequestsFromRows(rows), nil
}

// ClaimParty implements domain.DeliveryStore.
func (repo *DeliveryRepo) ClaimParty(ctx context.Context, claim domain.PartyClaim) (domain.PartyClaimResult, error) {
	var decision domain.PartyDecision
	result, err := repo.deliveries.ClaimParty(ctx, persistence.FeedPartyClaim{
		TokenID:      claim.TokenID,
		Repositories: claim.Repositories,
		DeliveredAt:  claim.DeliveredAt,
		Day:          claim.Day,
		ActiveSince:  claim.ActiveSince,
		Choose: func(snapshot persistence.FeedPartySnapshot) []persistence.FeedPullRequestKey {
			decision = repo.policy.Choose(partyRoundFromSnapshot(claim, snapshot))
			return persistenceKeys(decision.Take)
		},
	})
	if err != nil {
		return domain.PartyClaimResult{}, fmt.Errorf("feed: claim party pull requests: %w", err)
	}
	return domain.PartyClaimResult{
		Delivered: pullRequestsFromRows(result.Claimed),
		Taken:     result.Taken + decision.Taken,
		Waiting:   decision.Waiting,
	}, nil
}

func partyRoundFromSnapshot(claim domain.PartyClaim, snapshot persistence.FeedPartySnapshot) domain.PartyRound {
	round := domain.PartyRound{
		TokenID:      claim.TokenID,
		Now:          claim.DeliveredAt,
		PullRequests: make([]domain.WaitingPullRequest, 0, len(snapshot.Waiting)),
		Loads:        make(map[domain.PartyMember]int, len(snapshot.Loads)),
	}
	for _, load := range snapshot.Loads {
		round.Loads[domain.PartyMember{TokenID: load.TokenID, Repository: load.Repository}] = load.Taken
	}
	deliveriesByKey := make(map[persistence.FeedPullRequestKey][]persistence.FeedDelivery, len(snapshot.Waiting))
	for _, delivery := range snapshot.Deliveries {
		key := persistence.FeedPullRequestKey{Repository: delivery.Repository, Number: delivery.Number}
		deliveriesByKey[key] = append(deliveriesByKey[key], delivery)
	}
	for _, waiting := range snapshot.Waiting {
		key := persistence.FeedPullRequestKey{Repository: waiting.Repository, Number: waiting.Number}
		round.PullRequests = append(round.PullRequests, domain.WaitingPullRequest{
			Key:             domain.PullRequestKey{Repository: waiting.Repository, Number: waiting.Number},
			Party:           domain.PartyState{Reviewers: waiting.PartyReviewers, DeadlineAt: timeFromPointer(waiting.PartyDeadlineAt)},
			PartyDeliveries: countPartyDeliveries(deliveriesByKey[key]),
			Candidates:      candidatesWithoutDelivery(snapshot.Members[waiting.Repository], deliveriesByKey[key], claim.TokenID),
		})
	}
	return round
}

func countPartyDeliveries(deliveries []persistence.FeedDelivery) int {
	count := 0
	for _, delivery := range deliveries {
		if delivery.Party {
			count++
		}
	}
	return count
}

func candidatesWithoutDelivery(members []string, deliveries []persistence.FeedDelivery, claimantID string) []string {
	delivered := make(map[string]bool, len(deliveries)+1)
	delivered[claimantID] = true
	for _, delivery := range deliveries {
		delivered[delivery.TokenID] = true
	}
	var candidates []string
	for _, member := range members {
		if !delivered[member] {
			candidates = append(candidates, member)
			delivered[member] = true
		}
	}
	return candidates
}

func persistenceKeys(keys []domain.PullRequestKey) []persistence.FeedPullRequestKey {
	converted := make([]persistence.FeedPullRequestKey, 0, len(keys))
	for _, key := range keys {
		converted = append(converted, persistence.FeedPullRequestKey{Repository: key.Repository, Number: key.Number})
	}
	return converted
}
