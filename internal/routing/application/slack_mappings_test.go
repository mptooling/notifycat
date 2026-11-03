package application

import (
	"testing"

	"github.com/stretchr/testify/assert"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

func TestSlackMappings_RoutedTierWithOnlyPartyPathsHasNilPaths(t *testing.T) {
	reviewers := 2
	mappings := map[string]domain.Org{
		"acme": {"api": {
			Channel: "C0API000000",
			Paths: []domain.PathRule{
				{Dir: "billing", Party: domain.PartyOverride{Reviewers: &reviewers}},
			},
		}},
	}

	slackView := slackMappings(mappings)

	assert.Nil(t, slackView["acme"]["api"].Paths)
	assert.Len(t, mappings["acme"]["api"].Paths, 1, "the input must not change")
}
