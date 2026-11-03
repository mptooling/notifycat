package application

import "github.com/mptooling/notifycat/internal/feed/domain"

// TurnPolicy implements domain.TurnPolicy.
type TurnPolicy struct{}

// NewTurnPolicy builds a TurnPolicy.
func NewTurnPolicy() *TurnPolicy {
	return &TurnPolicy{}
}

// Choose implements domain.TurnPolicy.
func (policy *TurnPolicy) Choose(round domain.PartyRound) domain.PartyDecision {
	loads := make(map[domain.PartyMember]int, len(round.Loads))
	for member, load := range round.Loads {
		loads[member] = load
	}

	var decision domain.PartyDecision
	for _, pullRequest := range round.PullRequests {
		if pullRequest.PartyDeliveries >= pullRequest.Party.Reviewers {
			decision.Taken++
			continue
		}
		pollerMember := domain.PartyMember{TokenID: round.TokenID, Repository: pullRequest.Key.Repository}
		if round.Now.Before(pullRequest.Party.DeadlineAt) {
			lighterCandidates := 0
			for _, candidate := range pullRequest.Candidates {
				candidateMember := domain.PartyMember{TokenID: candidate, Repository: pullRequest.Key.Repository}
				if loads[candidateMember] < loads[pollerMember] {
					lighterCandidates++
				}
			}
			if lighterCandidates >= pullRequest.Party.Reviewers-pullRequest.PartyDeliveries {
				decision.Waiting++
				continue
			}
		}
		decision.Take = append(decision.Take, pullRequest.Key)
		loads[pollerMember]++
	}
	return decision
}
