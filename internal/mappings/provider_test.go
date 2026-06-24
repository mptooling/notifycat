package mappings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mptooling/notifycat/internal/store"
)

const badYAML = `
mappings:
  acme: !!invalid
`

func writeMappingsFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "mappings.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestProvider_Load_MissingFile_ReturnsFileNotFoundError(t *testing.T) {
	_, err := Load("/no/such/path/mappings.yaml")
	if err == nil {
		t.Fatal("Load() succeeded with missing file; want error")
	}
	var nfe *FileNotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("Load() error = %T(%v); want *FileNotFoundError", err, err)
	}
	if nfe.Path != "/no/such/path/mappings.yaml" {
		t.Errorf("FileNotFoundError.Path = %q; want /no/such/path/mappings.yaml", nfe.Path)
	}
	if !errors.Is(nfe.Err, os.ErrNotExist) {
		t.Errorf("FileNotFoundError.Err = %v; want os.ErrNotExist", nfe.Err)
	}
}

func TestProvider_Load_MalformedFile_ReturnsParseError(t *testing.T) {
	path := writeMappingsFile(t, badYAML)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() succeeded with malformed YAML; want error")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("Load() error = %T(%v); want *ParseError", err, err)
	}
	if pe.Path != path {
		t.Errorf("ParseError.Path = %q; want %q", pe.Path, path)
	}
	if pe.Err == nil {
		t.Error("ParseError.Err is nil")
	}
}

func tierProvider() *Provider {
	return NewProvider(map[string]Org{
		"acme": {
			"api": {Channel: "C0API", Mentions: []string{"<@U1>"}, MentionsPresent: true},
			"*":   {Channel: "C0DEFAULT"},
		},
	}, nil)
}

func TestGet_ExplicitRepo(t *testing.T) {
	got, err := tierProvider().Get(context.Background(), "acme/api")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SlackChannel != "C0API" || len(got.Mentions) != 1 || got.Mentions[0] != "<@U1>" {
		t.Errorf("got %+v; want C0API + [<@U1>]", got)
	}
}

func TestGet_WildcardFallback(t *testing.T) {
	got, err := tierProvider().Get(context.Background(), "acme/unlisted")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SlackChannel != "C0DEFAULT" {
		t.Errorf("channel = %q; want C0DEFAULT", got.SlackChannel)
	}
	if len(got.Mentions) != 1 || got.Mentions[0] != ChannelMention {
		t.Errorf("mentions = %v; want @channel default", got.Mentions)
	}
}

func TestGet_NoOrgOrNoTier(t *testing.T) {
	p := NewProvider(map[string]Org{
		"acme": {"api": {Channel: "C0API"}}, // no "*"
	}, nil)
	if _, err := p.Get(context.Background(), "acme/other"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v; want ErrNotFound (no tier, no wildcard)", err)
	}
	if _, err := p.Get(context.Background(), "ghost/api"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v; want ErrNotFound (no org)", err)
	}
}

func TestNewProvider_BehavesLikeLoad(t *testing.T) {
	m := map[string]Org{
		"acme": {
			"web": {Channel: "C0123ABCDE", Mentions: []string{"<@U1>"}, MentionsPresent: true},
		},
	}
	p := NewProvider(m, nil)

	got, err := p.Get(context.Background(), "acme/web")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SlackChannel != "C0123ABCDE" {
		t.Errorf("SlackChannel = %q; want C0123ABCDE", got.SlackChannel)
	}
	// nil digest → feature on by default with the default schedule.
	if d := p.Digest(); !d.Enabled || d.Schedule != DefaultDigestSchedule {
		t.Errorf("Digest() = %+v; want enabled with default schedule", d)
	}
	if len(p.Entries()) != 1 {
		t.Errorf("Entries() = %d; want 1", len(p.Entries()))
	}
}
