package config_test

import (
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/platform/config"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

func loadMappings(t *testing.T, mappings string) config.Config {
	t.Helper()
	writeConfig(t, "git_provider: github\n"+mappings)
	setSecrets(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return cfg
}

func loadMappingsError(t *testing.T, mappings string) string {
	t.Helper()
	writeConfig(t, "git_provider: github\n"+mappings)
	setSecrets(t)
	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() succeeded; want a fail-fast error")
	}
	return err.Error()
}

func tier(t *testing.T, cfg config.Config, org, repo string) routingdomain.RepoConfig {
	t.Helper()
	rc, ok := cfg.Mappings[org][repo]
	if !ok {
		t.Fatalf("mappings[%q][%q] missing; got %+v", org, repo, cfg.Mappings)
	}
	return rc
}

func TestLoad_PathRules_DirectoryKeys(t *testing.T) {
	cfg := loadMappings(t, `
mappings:
  acme:
    the-monorepo:
      channel: C0MONO00000
      mentions: ["<!subteam^S0ENG>"]
      paths:
        "/modules/acme":
          mentions: ["<!subteam^S0TEAMA>"]
        "/src/AuthBundle":
          channel: C0AUTH00000
          mentions: ["<!subteam^S0AUTH>"]
        "/vendor":
          mentions: []
`)

	rules := tier(t, cfg, "acme", "the-monorepo").Paths
	if len(rules) != 3 {
		t.Fatalf("got %d path rules; want 3: %+v", len(rules), rules)
	}
	byDir := map[string]routingdomain.PathRule{}
	for _, rule := range rules {
		byDir[rule.Dir] = rule
	}
	for _, dir := range []string{"modules/acme", "src/AuthBundle", "vendor"} {
		if _, ok := byDir[dir]; !ok {
			t.Errorf("path rule for %q missing; got dirs %+v", dir, byDir)
		}
	}
	if got := byDir["src/AuthBundle"].Channel; got != "C0AUTH00000" {
		t.Errorf("src/AuthBundle channel = %q; want C0AUTH00000", got)
	}
	if got := byDir["modules/acme"].Channel; got != "" {
		t.Errorf("modules/acme channel = %q; want empty (inherits the tier channel)", got)
	}
	vendor := byDir["vendor"]
	if !vendor.MentionsPresent || len(vendor.Mentions) != 0 {
		t.Errorf("vendor mentions = %+v present=%v; want an explicit empty list", vendor.Mentions, vendor.MentionsPresent)
	}
}

func TestLoad_TierMentionsEmptyList_PingsNobody(t *testing.T) {
	cfg := loadMappings(t, `
mappings:
  acme:
    api:
      channel: C0AAA0001
      mentions: []
`)

	rc := tier(t, cfg, "acme", "api")
	if !rc.MentionsPresent || len(rc.Mentions) != 0 {
		t.Errorf("mentions = %+v present=%v; want an explicit empty list", rc.Mentions, rc.MentionsPresent)
	}
}

func TestLoad_DigestSectionWithoutEnabled_StaysEnabled(t *testing.T) {
	cfg := loadMappings(t, "digest:\n  schedule: \"0 8 * * *\"\n")

	if cfg.Digest == nil {
		t.Fatal("cfg.Digest = nil; want the parsed section")
	}
	if !cfg.Digest.Enabled {
		t.Error("digest.enabled = false; want true when the key is absent")
	}
	if cfg.Digest.Schedule != "0 8 * * *" {
		t.Errorf("digest.schedule = %q; want 0 8 * * *", cfg.Digest.Schedule)
	}
}

func TestLoad_RejectedMappings(t *testing.T) {
	cases := []struct {
		name     string
		mappings string
		want     string
	}{
		{
			name:     "mentions null",
			mappings: "mappings:\n  acme:\n    api:\n      channel: C0AAA0001\n      mentions: null\n",
			want:     "null is not allowed",
		},
		{
			name:     "colliding path keys",
			mappings: "mappings:\n  acme:\n    api:\n      channel: C0AAA0001\n      paths:\n        \"/services/pay\":\n          channel: C0PAY0001\n        \"services/pay/\":\n          channel: C0PAY0002\n",
			want:     "same directory",
		},
		{
			name:     "path key escaping the repo",
			mappings: "mappings:\n  acme:\n    api:\n      channel: C0AAA0001\n      paths:\n        \"../etc\":\n          channel: C0PAY0001\n",
			want:     "..",
		},
		{
			name:     "per-tier digest timezone",
			mappings: "mappings:\n  acme:\n    api:\n      channel: C0AAA0001\n      digest:\n        timezone: Europe/Kyiv\n",
			want:     "only valid in the global digest section",
		},
		{
			name:     "unknown tier key",
			mappings: "mappings:\n  acme:\n    api:\n      channel: C0AAA0001\n      chanel: C0TYPO001\n",
			want:     "unknown field",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := loadMappingsError(t, tc.mappings)
			if !strings.Contains(msg, tc.want) {
				t.Errorf("error = %q; want it to mention %q", msg, tc.want)
			}
		})
	}
}
