package application_test

import (
	"context"
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/routing/application"
	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

func boolPointer(v bool) *bool { return &v }

// TestProviderEntriesHashesUnaffectedByAIConfig verifies the lock-invariance
// property: Entries() hashes must be identical whether or not per-tier ai:
// blocks are configured. AI settings live in RepoConfig.AI (which feeds
// AIEnabled/AIInstructions in the resolved RepoMapping) but are deliberately
// excluded from domain.Entry's hash payload — so operators can freely add or
// change per-tier AI guidance without invalidating their config.lock.
func TestProviderEntriesHashesUnaffectedByAIConfig(t *testing.T) {
	defaults := domain.Defaults{GitProvider: kernel.ProviderGitHub}
	baseOrg := map[string]domain.Org{
		"acme": {
			"*":   {Channel: "C0000000001"},
			"api": {Channel: "C0000000002"},
		},
	}
	withAIOrg := map[string]domain.Org{
		"acme": {
			"*":   {Channel: "C0000000001", AI: &domain.AIOverride{Enabled: boolPointer(true), Instructions: "be concise"}},
			"api": {Channel: "C0000000002", AI: &domain.AIOverride{Enabled: boolPointer(false), Instructions: "skip for this repo"}},
		},
	}

	baseEntries := application.NewProvider(defaults, baseOrg, nil).Entries()
	aiEntries := application.NewProvider(defaults, withAIOrg, nil).Entries()

	if len(baseEntries) != len(aiEntries) {
		t.Fatalf("entry count mismatch: %d vs %d", len(baseEntries), len(aiEntries))
	}
	baseHashes := make(map[string]bool, len(baseEntries))
	for _, entry := range baseEntries {
		baseHashes[entry.Hash()] = true
	}
	for _, entry := range aiEntries {
		if !baseHashes[entry.Hash()] {
			t.Errorf("entry %s/%s hash changed when AI config was added: adding per-tier ai: must not invalidate the lock", entry.Org, entry.Repo)
		}
	}
}

func TestProviderResolvesAIOverrides(t *testing.T) {
	defaults := domain.Defaults{AIEnabled: true, AIInstructions: "global guidance"}
	mappings := map[string]domain.Org{
		"acme": {
			"*":   {Channel: "C0000000001", AI: &domain.AIOverride{Instructions: "org guidance"}},
			"api": {AI: &domain.AIOverride{Enabled: boolPointer(false), Instructions: "repo guidance"}},
			"web": {},
		},
	}
	provider := application.NewProvider(defaults, mappings, nil)

	api, err := provider.Get(context.Background(), "acme/api")
	if err != nil {
		t.Fatal(err)
	}
	if api.AIEnabled {
		t.Error("acme/api sets ai.enabled: false; resolved mapping must be disabled")
	}
	if want := "global guidance\n\norg guidance\n\nrepo guidance"; api.AIInstructions != want {
		t.Errorf("AIInstructions = %q\nwant %q", api.AIInstructions, want)
	}

	web, err := provider.Get(context.Background(), "acme/web")
	if err != nil {
		t.Fatal(err)
	}
	if !web.AIEnabled {
		t.Error("acme/web inherits the enabled default")
	}
	if want := "global guidance\n\norg guidance"; web.AIInstructions != want {
		t.Errorf("AIInstructions = %q\nwant %q", web.AIInstructions, want)
	}
}
