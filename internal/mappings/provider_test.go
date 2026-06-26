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
	return NewProvider(Defaults{}, map[string]Org{
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
	p := NewProvider(Defaults{}, map[string]Org{
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
	p := NewProvider(Defaults{}, m, nil)

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

func TestEntries_PerTierWithResolvedChannel(t *testing.T) {
	p := NewProvider(Defaults{}, map[string]Org{
		"acme": {
			"web": {}, // inherits channel from "*"
			"api": {Channel: "C0API"},
			"*":   {Channel: "C0DEFAULT"},
		},
	}, nil)
	entries := p.Entries()
	// deterministic order: explicit repos A→Z then wildcard last
	if len(entries) != 3 {
		t.Fatalf("entries = %d; want 3", len(entries))
	}
	if entries[0].Key() != "acme/api" || entries[0].Channel != "C0API" {
		t.Errorf("entries[0] = %+v; want acme/api C0API", entries[0])
	}
	if entries[1].Key() != "acme/web" || entries[1].Channel != "C0DEFAULT" {
		t.Errorf("entries[1] = %+v; want acme/web resolved C0DEFAULT", entries[1])
	}
	if !entries[2].Wildcard || entries[2].Key() != "acme/*" || entries[2].Channel != "C0DEFAULT" {
		t.Errorf("entries[2] = %+v; want acme/* C0DEFAULT", entries[2])
	}
}

func TestGet_PopulatesResolvedBehavior(t *testing.T) {
	global := Defaults{
		Reactions:        store.Reactions{Enabled: true, NewPR: "eyes", Approved: "white_check_mark"},
		DependabotFormat: true,
	}
	shipit := "shipit"
	p := NewProvider(global, map[string]Org{
		"acme": {
			"api": {Channel: "C0API", Reactions: &ReactionsOverride{Approved: &shipit}},
			"*":   {Channel: "C0DEFAULT"},
		},
	}, nil)
	got, err := p.Get(context.Background(), "acme/api")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SlackChannel != "C0API" {
		t.Errorf("channel = %q", got.SlackChannel)
	}
	if got.Reactions.Approved != "shipit" {
		t.Errorf("approved = %q; want repo override shipit", got.Reactions.Approved)
	}
	if got.Reactions.NewPR != "eyes" || !got.DependabotFormat {
		t.Errorf("global defaults lost: %+v dependabot=%v", got.Reactions, got.DependabotFormat)
	}
}

func TestDigestFor_RepoOverridesGlobal(t *testing.T) {
	weekdays := "0 8 * * 1-5"
	p := NewProvider(Defaults{}, map[string]Org{
		"acme": {
			"web": {Channel: "C0WEB", Digest: &DigestConfig{Enabled: true, Schedule: weekdays}},
			"*":   {Channel: "C0DEFAULT"},
		},
	}, nil) // global digest absent → default on, 9am
	d := p.DigestFor("acme/web")
	if !d.Enabled || d.Schedule != weekdays {
		t.Errorf("web digest = %+v; want enabled weekdays", d)
	}
	dd := p.DigestFor("acme/other") // matches "*", no digest override → global default
	if !dd.Enabled || dd.Schedule != DefaultDigestSchedule {
		t.Errorf("default digest = %+v; want global default", dd)
	}
}

func TestSchedules_DistinctEnabledOnly(t *testing.T) {
	weekdays := "0 8 * * 1-5"
	off := false
	p := NewProvider(Defaults{}, map[string]Org{
		"acme": {
			"web":  {Channel: "C0WEB", Digest: &DigestConfig{Enabled: true, Schedule: weekdays}},
			"api":  {Channel: "C0API"}, // global default schedule
			"mute": {Channel: "C0MUTE", Digest: &DigestConfig{Enabled: off}},
			"*":    {Channel: "C0DEFAULT"},
		},
	}, nil)
	got := p.Schedules()
	// weekdays + default 9am; "mute" disabled contributes nothing
	want := map[string]bool{weekdays: true, DefaultDigestSchedule: true}
	if len(got) != 2 || !want[got[0]] || !want[got[1]] {
		t.Errorf("Schedules() = %v; want the two distinct enabled schedules", got)
	}
}
