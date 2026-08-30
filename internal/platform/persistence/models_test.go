package persistence_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func TestTableNames(t *testing.T) {
	assert.Equal(t, "pull_requests", persistence.PullRequest{}.TableName())
	assert.Equal(t, "messages", persistence.Message{}.TableName())
}

func TestRepoMapping_CarriesBehavioralConfig(t *testing.T) {
	mapping := persistence.RepoMapping{
		Repository:       "o/r",
		SlackChannel:     "C0",
		Reactions:        persistence.Reactions{Enabled: true, NewPR: "eyes", Approved: "shipit"},
		IgnoreAIReviews:  true,
		DependabotFormat: false,
	}

	assert.True(t, mapping.Reactions.Enabled)
	assert.Equal(t, "shipit", mapping.Reactions.Approved)
	assert.True(t, mapping.IgnoreAIReviews)
	assert.False(t, mapping.DependabotFormat)
}
