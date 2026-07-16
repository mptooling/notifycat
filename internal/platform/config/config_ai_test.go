package config_test

import (
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/platform/config"
	saliencedomain "github.com/mptooling/notifycat/internal/salience/domain"
)

func TestLoad_AIDefaultsOff(t *testing.T) {
	writeWireConfig(t, "git_provider: github\n")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.Enabled {
		t.Error("ai must default to disabled")
	}
}

func TestLoad_AIGeminiHappyPath(t *testing.T) {
	writeWireConfig(t, `
git_provider: github
ai:
  enabled: true
  provider: gemini
  model: gemini-2.5-flash
  instructions: |
    Changes under /billing are payment-critical.
`)
	t.Setenv("AI_API_KEY", "test-key")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AI.Provider != saliencedomain.ProviderGemini || cfg.AI.Model != "gemini-2.5-flash" {
		t.Errorf("AI = %+v", cfg.AI)
	}
	if !strings.Contains(cfg.AI.Instructions, "payment-critical") {
		t.Errorf("Instructions = %q", cfg.AI.Instructions)
	}
	if cfg.AIAPIKey.Reveal() != "test-key" {
		t.Error("AI_API_KEY not read")
	}
	if cfg.AIAPIKey.String() == "test-key" {
		t.Error("AIAPIKey renders raw via String(); must be Secret-typed")
	}
}

func TestLoad_AIValidation(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		key     string
		wantErr string
	}{
		{"unknown provider", "ai:\n  enabled: true\n  provider: anthropic\n  model: m\n", "k", "ai.provider"},
		{"enabled without model", "ai:\n  enabled: true\n  provider: gemini\n", "k", "ai.model"},
		{"gemini without key", "ai:\n  enabled: true\n  provider: gemini\n  model: m\n", "", "AI_API_KEY"},
		{"openai_compatible without base_url", "ai:\n  enabled: true\n  provider: openai_compatible\n  model: m\n", "", "ai.base_url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeWireConfig(t, "git_provider: github\n"+tc.yaml)
			t.Setenv("AI_API_KEY", tc.key)
			_, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load() error = %v; want mention of %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoad_AIOpenAICompatibleKeyless(t *testing.T) {
	writeWireConfig(t, `
git_provider: github
ai:
  enabled: true
  provider: openai_compatible
  model: llama3
  base_url: http://localhost:11434/v1
`)
	t.Setenv("AI_API_KEY", "")
	if _, err := config.Load(); err != nil {
		t.Fatalf("keyless openai_compatible must boot (local endpoints run keyless); got %v", err)
	}
}

func TestLoad_DisabledAIBlockIsNotValidated(t *testing.T) {
	writeWireConfig(t, "git_provider: github\nai:\n  enabled: false\n  provider: junk\n")
	if _, err := config.Load(); err != nil {
		t.Fatalf("a disabled ai block must not fail validation; got %v", err)
	}
}
