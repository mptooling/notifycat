package infrastructure_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/routing/application"
	domain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/routing/infrastructure"
)

// providerDoc parses a mappings document and wraps it in a Provider so the
// resolution path can be exercised end to end.
func providerDoc(t *testing.T, body string) *application.Provider {
	t.Helper()
	f, err := infrastructure.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return application.NewProvider(domain.Defaults{}, f.Mappings, nil)
}

// monorepoDoc is a six-path tier used by most resolution tests:
//
//	modules/acme     → @U0A (channel inherits base)
//	modules/betta    → @U0B (channel inherits base)
//	config           → @U0A, @U0B
//	src/AuthBundle   → own channel C0AUTH00000, @U0AUTH
//	vendor           → silent ([])
//	docs             → inherits base mentions (no key)
const monorepoDoc = "mappings:\n" +
	"  acme:\n" +
	"    the-monorepo:\n" +
	"      channel: C0BASE00000\n" +
	"      mentions: [\"<!subteam^S0ENG>\"]\n" +
	"      paths:\n" +
	"        \"/modules/acme\": {mentions: [\"<@U0A>\"]}\n" +
	"        \"/modules/betta\": {mentions: [\"<@U0B>\"]}\n" +
	"        \"/config\": {mentions: [\"<@U0A>\", \"<@U0B>\"]}\n" +
	"        \"/src/AuthBundle\": {channel: C0AUTH00000, mentions: [\"<@U0AUTH>\"]}\n" +
	"        \"/vendor\": {mentions: []}\n" +
	"        \"/docs\": {}\n"

func TestTargetsForFiles_FanOutPerChannel(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	got := p.TargetsForFiles("acme/the-monorepo", []string{"modules/acme/x.go", "src/AuthBundle/y.go"})
	// modules/acme inherits base channel C0BASE00000; src/AuthBundle has its own.
	want := map[string][]string{
		"C0BASE00000": {"<@U0A>"},
		"C0AUTH00000": {"<@U0AUTH>"},
	}
	if len(got) != 2 {
		t.Fatalf("got %d targets; want 2: %+v", len(got), got)
	}
	for _, tg := range got {
		if !slices.Equal(tg.Mentions, want[tg.Channel]) {
			t.Errorf("channel %s mentions = %v; want %v", tg.Channel, tg.Mentions, want[tg.Channel])
		}
	}
}

func TestTargetsForFiles_MentionsUnionWithinChannel(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	// modules/acme (@U0A) + config (@U0A,@U0B) both inherit the base channel.
	got := p.TargetsForFiles("acme/the-monorepo", []string{"modules/acme/x.go", "config/app.yaml"})
	if len(got) != 1 || got[0].Channel != "C0BASE00000" {
		t.Fatalf("want one base-channel target; got %+v", got)
	}
	if !slices.Equal(got[0].Mentions, []string{"<@U0A>", "<@U0B>"}) {
		t.Errorf("mentions = %v; want deduped union [<@U0A> <@U0B>]", got[0].Mentions)
	}
}

func TestTargetsForFiles_NoMatchReturnsBase(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	got := p.TargetsForFiles("acme/the-monorepo", []string{"README.md"})
	if len(got) != 1 || got[0].Channel != "C0BASE00000" ||
		!slices.Equal(got[0].Mentions, []string{"<!subteam^S0ENG>"}) {
		t.Fatalf("no match should yield single base target; got %+v", got)
	}
}

func TestHasPathRules(t *testing.T) {
	with := providerDoc(t, monorepoDoc)
	if !with.HasPathRules() {
		t.Error("HasPathRules() = false; want true")
	}
	without := providerDoc(t, "mappings:\n  acme:\n    plain:\n      channel: C0PLAIN0000\n")
	if without.HasPathRules() {
		t.Error("HasPathRules() = true; want false")
	}
}

func TestRepoHasPathRules(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	if !p.RepoHasPathRules("acme/the-monorepo") {
		t.Error("RepoHasPathRules(acme/the-monorepo) = false; want true")
	}
	if p.RepoHasPathRules("acme/other") {
		t.Error("RepoHasPathRules(acme/other) = true; want false (unmapped)")
	}
	plain := providerDoc(t, "mappings:\n  acme:\n    plain:\n      channel: C0PLAIN0000\n")
	if plain.RepoHasPathRules("acme/plain") {
		t.Error("RepoHasPathRules(acme/plain) = true; want false (no paths)")
	}
}

func TestPathChannels_DistinctSorted(t *testing.T) {
	doc := "mappings:\n  acme:\n    mono:\n      channel: C0BASE00000\n      paths:\n" +
		"        \"/a\": {channel: C0ZZZ00000}\n" +
		"        \"/b\": {channel: C0AAA00000}\n" +
		"        \"/c\": {channel: C0AAA00000}\n" + // duplicate
		"        \"/d\": {mentions: []}\n" // no channel → not listed
	p := providerDoc(t, doc)
	got := p.PathChannels("acme/mono")
	if !slices.Equal(got, []string{"C0AAA00000", "C0ZZZ00000"}) {
		t.Errorf("PathChannels = %v; want sorted distinct [C0AAA00000 C0ZZZ00000]", got)
	}
	if p.PathChannels("acme/unmapped") != nil {
		t.Error("PathChannels(unmapped) should be nil")
	}
}

// Ensure domain.Target is used by the package (compile-time check).
var _ domain.Target
