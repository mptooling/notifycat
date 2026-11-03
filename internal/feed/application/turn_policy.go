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
	for _, waiting := range round.PullRequests {
		if waiting.PartyDeliveries >= waiting.Party.Reviewers {
			decision.Taken++
			continue
		}
		pollerMember := domain.PartyMember{TokenID: round.TokenID, Repository: waiting.Key.Repository}
		if round.Now.Before(waiting.Party.DeadlineAt) {
			lighter := 0
			for _, candidate := range waiting.Candidates {
				candidateMember := domain.PartyMember{TokenID: candidate, Repository: waiting.Key.Repository}
				if loads[candidateMember] < loads[pollerMember] {
					lighter++
				}
			}
			if lighter >= waiting.Party.Reviewers-waiting.PartyDeliveries {
				decision.Waiting++
				continue
			}
		}
		decision.Take = append(decision.Take, waiting.Key)
		loads[pollerMember]++
	}
	return decision
}
