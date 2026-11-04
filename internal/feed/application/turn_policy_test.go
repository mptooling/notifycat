package application_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/feed/application"
	"github.com/mptooling/notifycat/internal/feed/domain"
)

func TestTurnPolicy_Choose(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	web := func(number int) domain.PullRequestKey {
		return domain.PullRequestKey{Repository: "acme/web", Number: number}
	}
	api := func(number int) domain.PullRequestKey {
		return domain.PullRequestKey{Repository: "acme/api", Number: number}
	}
	waitingPullRequest := func(key domain.PullRequestKey, reviewers, deliveries int, deadline time.Time, candidates ...string) domain.WaitingPullRequest {
		return domain.WaitingPullRequest{
			Key:             key,
			Party:           domain.PartyState{Reviewers: reviewers, RecordedAt: now, DeadlineAt: deadline},
			PartyDeliveries: deliveries,
			Candidates:      candidates,
		}
	}
	member := func(token, repository string) domain.PartyMember {
		return domain.PartyMember{TokenID: token, Repository: repository}
	}

	testCases := []struct {
		name         string
		pullRequests []domain.WaitingPullRequest
		loads        map[domain.PartyMember]int
		want         domain.PartyDecision
	}{
		{
			name:         "nobody else takes it",
			pullRequests: []domain.WaitingPullRequest{waitingPullRequest(web(1), 1, 0, future)},
			want:         domain.PartyDecision{Take: []domain.PullRequestKey{web(1)}},
		},
		{
			name:         "a pull request at its cap is not taken",
			pullRequests: []domain.WaitingPullRequest{waitingPullRequest(web(1), 1, 1, future)},
			want:         domain.PartyDecision{Taken: 1},
		},
		{
			name:         "a pull request at its cap stays taken after the timer ran out",
			pullRequests: []domain.WaitingPullRequest{waitingPullRequest(web(1), 1, 1, now, "bob")},
			want:         domain.PartyDecision{Taken: 1},
		},
		{
			name:         "a pull request above its cap is not taken",
			pullRequests: []domain.WaitingPullRequest{waitingPullRequest(web(1), 1, 2, future)},
			want:         domain.PartyDecision{Taken: 1},
		},
		{
			name:         "a lighter candidate has the turn",
			pullRequests: []domain.WaitingPullRequest{waitingPullRequest(web(1), 1, 0, future, "bob")},
			loads:        map[domain.PartyMember]int{member("alice", "acme/web"): 3, member("bob", "acme/web"): 1},
			want:         domain.PartyDecision{Waiting: 1},
		},
		{
			name:         "equal loads take it",
			pullRequests: []domain.WaitingPullRequest{waitingPullRequest(web(1), 1, 0, future, "bob")},
			loads:        map[domain.PartyMember]int{member("alice", "acme/web"): 2, member("bob", "acme/web"): 2},
			want:         domain.PartyDecision{Take: []domain.PullRequestKey{web(1)}},
		},
		{
			name:         "cap 3 with two lighter candidates takes it",
			pullRequests: []domain.WaitingPullRequest{waitingPullRequest(web(1), 3, 0, future, "bob", "carol")},
			loads:        map[domain.PartyMember]int{member("alice", "acme/web"): 2},
			want:         domain.PartyDecision{Take: []domain.PullRequestKey{web(1)}},
		},
		{
			name:         "cap 2 with no delivery and two lighter candidates waits",
			pullRequests: []domain.WaitingPullRequest{waitingPullRequest(web(1), 2, 0, future, "bob", "carol")},
			loads:        map[domain.PartyMember]int{member("alice", "acme/web"): 2},
			want:         domain.PartyDecision{Waiting: 1},
		},
		{
			name:         "cap 2 with one delivery and one lighter candidate waits",
			pullRequests: []domain.WaitingPullRequest{waitingPullRequest(web(1), 2, 1, future, "bob")},
			loads:        map[domain.PartyMember]int{member("alice", "acme/web"): 2},
			want:         domain.PartyDecision{Waiting: 1},
		},
		{
			name:         "a deadline equal to now has run out",
			pullRequests: []domain.WaitingPullRequest{waitingPullRequest(web(1), 1, 0, now, "bob")},
			loads:        map[domain.PartyMember]int{member("alice", "acme/web"): 2},
			want:         domain.PartyDecision{Take: []domain.PullRequestKey{web(1)}},
		},
		{
			name:         "a zero deadline has run out",
			pullRequests: []domain.WaitingPullRequest{waitingPullRequest(web(1), 1, 0, time.Time{}, "bob")},
			loads:        map[domain.PartyMember]int{member("alice", "acme/web"): 2},
			want:         domain.PartyDecision{Take: []domain.PullRequestKey{web(1)}},
		},
		{
			name: "the load rises inside one round",
			pullRequests: []domain.WaitingPullRequest{
				waitingPullRequest(web(1), 1, 0, future, "bob"),
				waitingPullRequest(web(2), 1, 0, future, "bob"),
			},
			want: domain.PartyDecision{Take: []domain.PullRequestKey{web(1)}, Waiting: 1},
		},
		{
			name: "load is counted per repository",
			pullRequests: []domain.WaitingPullRequest{
				waitingPullRequest(web(1), 1, 0, future, "bob"),
				waitingPullRequest(api(1), 1, 0, future, "bob"),
			},
			loads: map[domain.PartyMember]int{
				member("alice", "acme/web"): 5,
				member("bob", "acme/web"):   1,
				member("bob", "acme/api"):   1,
			},
			want: domain.PartyDecision{Take: []domain.PullRequestKey{api(1)}, Waiting: 1},
		},
		{
			name: "take keeps the input order",
			pullRequests: []domain.WaitingPullRequest{
				waitingPullRequest(web(3), 1, 0, future),
				waitingPullRequest(api(7), 1, 0, future),
				waitingPullRequest(web(1), 1, 0, future),
			},
			want: domain.PartyDecision{Take: []domain.PullRequestKey{web(3), api(7), web(1)}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			round := domain.PartyRound{TokenID: "alice", Now: now, PullRequests: testCase.pullRequests, Loads: testCase.loads}

			got := application.NewTurnPolicy().Choose(round)

			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestTurnPolicy_Choose_KeepsCallerLoads(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	loads := map[domain.PartyMember]int{}
	round := domain.PartyRound{
		TokenID: "alice",
		Now:     now,
		PullRequests: []domain.WaitingPullRequest{{
			Key:   domain.PullRequestKey{Repository: "acme/web", Number: 1},
			Party: domain.PartyState{Reviewers: 1, DeadlineAt: now.Add(time.Hour)},
		}},
		Loads: loads,
	}

	application.NewTurnPolicy().Choose(round)

	assert.Empty(t, loads)
}
