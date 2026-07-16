package application_test

import (
	"context"
	"testing"

	"github.com/mptooling/notifycat/internal/routing/application"
	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

func boolPointer(v bool) *bool { return &v }

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
