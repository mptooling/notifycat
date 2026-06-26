package store_test

import (
	"testing"

	"github.com/mptooling/notifycat/internal/store"
)

func TestRepoMapping_CarriesBehavioralConfig(t *testing.T) {
	m := store.RepoMapping{
		Repository:       "o/r",
		SlackChannel:     "C0",
		Reactions:        store.Reactions{Enabled: true, NewPR: "eyes", Approved: "shipit"},
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
