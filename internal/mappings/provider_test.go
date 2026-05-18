package mappings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mptooling/notifycat/internal/store"
)

func writeMappingsFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "mappings.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestProvider_Get_ExactMatch(t *testing.T) {
	p, err := Load(writeMappingsFile(t, validYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, err := p.Get(context.Background(), "acme/api")
	if err != nil {
		t.Fatalf("get acme/api: %v", err)
	}
	if got.Repository != "acme/api" || got.SlackChannel != "C0123ABCDE" || len(got.Mentions) != 2 {
		t.Errorf("get acme/api: %+v", got)
	}
}

func TestProvider_Get_Wildcard(t *testing.T) {
	p, err := Load(writeMappingsFile(t, validYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, err := p.Get(context.Background(), "beta/anything")
	if err != nil {
		t.Fatalf("get beta/anything: %v", err)
	}
	if got.Repository != "beta/anything" || got.SlackChannel != "C0456FGHIJ" {
		t.Errorf("wildcard get: %+v", got)
	}
}

func TestProvider_Get_NotFound(t *testing.T) {
	p, err := Load(writeMappingsFile(t, validYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	_, err = p.Get(context.Background(), "other/repo")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown repo err = %v; want ErrNotFound", err)
	}
}

func TestProvider_Entries(t *testing.T) {
	p, err := Load(writeMappingsFile(t, validYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	entries := p.Entries()
	if len(entries) != 3 {
		t.Fatalf("entries = %d; want 3", len(entries))
	}
	keys := make(map[string]bool)
	for _, e := range entries {
		keys[e.Key()] = true
	}
	for _, want := range []string{"acme/api", "acme/web", "beta/*"} {
		if !keys[want] {
			t.Errorf("missing entry %q; got %v", want, keys)
		}
	}
}

func TestProvider_Load_BadFile(t *testing.T) {
	_, err := Load("/no/such/path/mappings.yaml")
	if err == nil {
		t.Fatal("expected error on missing file")
	}
}

const absentMentionsYAML = `
mappings:
  acme:
    channel: C0123ABCDE
    repositories: ["api"]
  beta:
    channel: C0456FGHIJ
    mentions: []
    repositories: ["web"]
`

func TestProvider_Get_AbsentMentions_FallsBackToChannel(t *testing.T) {
	p, err := Load(writeMappingsFile(t, absentMentionsYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, err := p.Get(context.Background(), "acme/api")
	if err != nil {
		t.Fatalf("get acme/api: %v", err)
	}
	if len(got.Mentions) != 1 || got.Mentions[0] != ChannelMention {
		t.Errorf("absent mentions = %v; want [%q]", got.Mentions, ChannelMention)
	}
}

func TestProvider_Get_EmptyMentions_StaysEmpty(t *testing.T) {
	p, err := Load(writeMappingsFile(t, absentMentionsYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, err := p.Get(context.Background(), "beta/web")
	if err != nil {
		t.Fatalf("get beta/web: %v", err)
	}
	if len(got.Mentions) != 0 {
		t.Errorf("empty mentions = %v; want empty", got.Mentions)
	}
}

func TestProvider_Entries_AbsentMentionsMaterialized(t *testing.T) {
	p, err := Load(writeMappingsFile(t, absentMentionsYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, e := range p.Entries() {
		switch e.Key() {
		case "acme/api":
			if len(e.Mentions) != 1 || e.Mentions[0] != ChannelMention {
				t.Errorf("acme/api mentions = %v; want [%q]", e.Mentions, ChannelMention)
			}
		case "beta/web":
			if len(e.Mentions) != 0 {
				t.Errorf("beta/web mentions = %v; want empty", e.Mentions)
			}
		}
	}
}
