package mappings_test

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/mappings"
)

// providerDoc parses a mappings document and wraps it in a Provider so the
// resolution path (GetForFiles) can be exercised end to end.
func providerDoc(t *testing.T, body string) *mappings.Provider {
	t.Helper()
	f, err := mappings.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return mappings.NewProvider(mappings.Defaults{}, f.Mappings, nil)
}

func testLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
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

func get(t *testing.T, p *mappings.Provider, logger *slog.Logger, files ...string) (channel string, mentions []string) {
	t.Helper()
	m, err := p.GetForFiles(context.Background(), logger, "acme/the-monorepo", files)
	if err != nil {
		t.Fatalf("GetForFiles: %v", err)
	}
	return m.SlackChannel, m.Mentions
}

func TestGetForFiles_SingleMatch(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	ch, ms := get(t, p, nil, "modules/acme/handler.go")
	if ch != "C0BASE00000" {
		t.Errorf("channel = %q; want inherited base C0BASE00000", ch)
	}
	if !slices.Equal(ms, []string{"<@U0A>"}) {
		t.Errorf("mentions = %v; want [<@U0A>]", ms)
	}
}

func TestGetForFiles_NestedMostSpecific(t *testing.T) {
	doc := "mappings:\n  acme:\n    the-monorepo:\n      channel: C0BASE00000\n      paths:\n" +
		"        \"/modules\": {channel: C0WIDE00000, mentions: [\"<@U0WIDE>\"]}\n" +
		"        \"/modules/acme\": {channel: C0DEEP00000, mentions: [\"<@U0DEEP>\"]}\n"
	p := providerDoc(t, doc)
	ch, ms := get(t, p, nil, "modules/acme/x.go")
	if ch != "C0DEEP00000" {
		t.Errorf("channel = %q; want C0DEEP00000 (longest prefix wins)", ch)
	}
	if !slices.Equal(ms, []string{"<@U0DEEP>"}) {
		t.Errorf("mentions = %v; want [<@U0DEEP>]", ms)
	}
}

func TestGetForFiles_CrossFileChannelWinner(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	// modules/acme (12 chars) vs src/AuthBundle (14 chars) → AuthBundle owns channel.
	ch, ms := get(t, p, nil, "modules/acme/x.go", "src/AuthBundle/y.go")
	if ch != "C0AUTH00000" {
		t.Errorf("channel = %q; want C0AUTH00000 (longest matched dir)", ch)
	}
	if !slices.Equal(ms, []string{"<@U0A>", "<@U0AUTH>"}) {
		t.Errorf("mentions = %v; want union [<@U0A> <@U0AUTH>]", ms)
	}
}

func TestGetForFiles_FewestSegmentsTieBreak(t *testing.T) {
	// Two winners of equal directory length: fewest segments wins.
	doc := "mappings:\n  acme:\n    the-monorepo:\n      channel: C0BASE00000\n      paths:\n" +
		"        \"/aa/bb\": {channel: C0SEG2000000}\n" +
		"        \"/abcde\": {channel: C0SEG1000000}\n"
	p := providerDoc(t, doc)
	ch, _ := get(t, p, nil, "aa/bb/x.go", "abcde/y.go")
	if ch != "C0SEG1000000" {
		t.Errorf("channel = %q; want C0SEG1000000 (equal length, fewer segments)", ch)
	}
}

func TestGetForFiles_MentionsUnionDedup(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	// config (@U0A,@U0B) + modules/acme (@U0A) → deduped union; channel inherits base
	// because modules/acme (12) is longer than config (6) and neither sets a channel.
	ch, ms := get(t, p, nil, "config/app.yaml", "modules/acme/x.go")
	if ch != "C0BASE00000" {
		t.Errorf("channel = %q; want base C0BASE00000", ch)
	}
	if !slices.Equal(ms, []string{"<@U0A>", "<@U0B>"}) {
		t.Errorf("mentions = %v; want deduped [<@U0A> <@U0B>]", ms)
	}
}

func TestGetForFiles_InheritBaseMentions(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	// docs has no mentions key → inherits the base mentions.
	_, ms := get(t, p, nil, "docs/readme.md")
	if !slices.Equal(ms, []string{"<!subteam^S0ENG>"}) {
		t.Errorf("mentions = %v; want inherited base [<!subteam^S0ENG>]", ms)
	}
}

func TestGetForFiles_SilentPath(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	// vendor has mentions: [] → ping nobody, channel inherits base.
	ch, ms := get(t, p, nil, "vendor/lib/x.go")
	if ch != "C0BASE00000" {
		t.Errorf("channel = %q; want base", ch)
	}
	if len(ms) != 0 {
		t.Errorf("mentions = %v; want [] (silent)", ms)
	}
}

func TestGetForFiles_NoMatchFallsBackToTier(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	ch, ms := get(t, p, nil, "README.md")
	if ch != "C0BASE00000" {
		t.Errorf("channel = %q; want base C0BASE00000", ch)
	}
	if !slices.Equal(ms, []string{"<!subteam^S0ENG>"}) {
		t.Errorf("mentions = %v; want base [<!subteam^S0ENG>]", ms)
	}
}

func TestGetForFiles_EmptyFilesFallsBack(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	ch, ms := get(t, p, nil)
	if ch != "C0BASE00000" || !slices.Equal(ms, []string{"<!subteam^S0ENG>"}) {
		t.Errorf("empty files: got (%q, %v); want base routing", ch, ms)
	}
}

// M3 — mentions bottom out at @channel and a warning is logged.
func TestGetForFiles_FallbackToChannelWarns(t *testing.T) {
	doc := "mappings:\n  acme:\n    the-monorepo:\n      channel: C0BASE00000\n      paths:\n" +
		"        \"/modules/acme\": {}\n" // no base mentions, path inherits → @channel
	p := providerDoc(t, doc)
	logger, buf := testLogger()
	ch, ms := get(t, p, logger, "modules/acme/x.go")
	if ch != "C0BASE00000" {
		t.Errorf("channel = %q; want base", ch)
	}
	if !slices.Equal(ms, []string{mappings.ChannelMention}) {
		t.Errorf("mentions = %v; want [%s]", ms, mappings.ChannelMention)
	}
	if !strings.Contains(buf.String(), "resolved to @channel") {
		t.Errorf("expected @channel warning; log was:\n%s", buf.String())
	}
}

// M5 — too many matched directories falls back to the base channel and warns.
func TestGetForFiles_SafetyValveWarns(t *testing.T) {
	p := providerDoc(t, monorepoDoc) // six path rules
	logger, buf := testLogger()
	ch, ms := get(t, p, logger,
		"modules/acme/a", "modules/betta/b", "config/c",
		"src/AuthBundle/d", "vendor/e", "docs/f") // matches all six > cap of 5
	if ch != "C0BASE00000" {
		t.Errorf("channel = %q; want base C0BASE00000 (valve tripped)", ch)
	}
	if !slices.Equal(ms, []string{"<!subteam^S0ENG>"}) {
		t.Errorf("mentions = %v; want base mentions", ms)
	}
	if !strings.Contains(buf.String(), "too many matched directories") {
		t.Errorf("expected safety-valve warning; log was:\n%s", buf.String())
	}
}

func TestGetForFiles_SegmentAwareNoFalsePrefix(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	// "modules/acmexyz" must NOT match the "modules/acme" rule.
	ch, ms := get(t, p, nil, "modules/acmexyz/x.go")
	if ch != "C0BASE00000" || !slices.Equal(ms, []string{"<!subteam^S0ENG>"}) {
		t.Errorf("acmexyz: got (%q, %v); want base routing (no false prefix match)", ch, ms)
	}
}

func TestGetForFiles_NoPathsEqualsGet(t *testing.T) {
	doc := "mappings:\n  acme:\n    plain:\n      channel: C0PLAIN0000\n      mentions: [\"<@U0P>\"]\n"
	p := providerDoc(t, doc)
	m, err := p.GetForFiles(context.Background(), nil, "acme/plain", []string{"any/file.go"})
	if err != nil {
		t.Fatalf("GetForFiles: %v", err)
	}
	if m.SlackChannel != "C0PLAIN0000" || !slices.Equal(m.Mentions, []string{"<@U0P>"}) {
		t.Errorf("no-paths repo: got (%q, %v); want plain tier routing", m.SlackChannel, m.Mentions)
	}
}

func TestGetForFiles_UnmappedRepoErrors(t *testing.T) {
	p := providerDoc(t, monorepoDoc)
	if _, err := p.GetForFiles(context.Background(), nil, "acme/unknown", []string{"x"}); err == nil {
		t.Fatal("expected error for unmapped repo")
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
