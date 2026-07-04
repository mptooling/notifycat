package persistence_test

import (
	"testing"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func TestPullRequestTableName(t *testing.T) {
	if (persistence.PullRequest{}).TableName() != "pull_requests" {
		t.Errorf("PullRequest.TableName = %q; want pull_requests", (persistence.PullRequest{}).TableName())
	}
	if (persistence.Message{}).TableName() != "messages" {
		t.Errorf("Message.TableName = %q; want messages", (persistence.Message{}).TableName())
	}
}

func TestRepoMapping_CarriesBehavioralConfig(t *testing.T) {
	m := persistence.RepoMapping{
		Repository:       "o/r",
		SlackChannel:     "C0",
		Reactions:        persistence.Reactions{Enabled: true, NewPR: "eyes", Approved: "shipit"},
		IgnoreAIReviews:  true,
		DependabotFormat: false,
	}
	if !m.Reactions.Enabled || m.Reactions.Approved != "shipit" {
		t.Errorf("reactions not carried: %+v", m.Reactions)
	}
	if !m.IgnoreAIReviews || m.DependabotFormat {
		t.Errorf("toggles not carried: ignore=%v dependabot=%v", m.IgnoreAIReviews, m.DependabotFormat)
	}
}
