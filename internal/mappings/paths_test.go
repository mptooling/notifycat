package mappings_test

import (
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/mappings"
)

// parseDoc runs the full Parse pipeline (decode + structural validation) on a
// mappings document, returning the File and any error.
func parseDoc(t *testing.T, body string) (mappings.File, error) {
	t.Helper()
	return mappings.Parse(strings.NewReader(body))
}

const baseRepoHead = "mappings:\n  acme:\n    the-monorepo:\n      channel: C0BASE0000\n"

func TestPaths_ParsedInDeclarationOrder(t *testing.T) {
	f, err := parseDoc(t, baseRepoHead+
		"      paths:\n"+
		"        \"/modules/acme\": {mentions: [\"<@U1>\"]}\n"+
		"        \"/src/AuthBundle/\": {channel: C0AUTH0000, mentions: [\"<@U2>\"]}\n"+
		"        \"config\": {mentions: []}\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rc := f.Mappings["acme"]["the-monorepo"]
	if len(rc.Paths) != 3 {
		t.Fatalf("got %d path rules; want 3 (%+v)", len(rc.Paths), rc.Paths)
	}
	wantDirs := []string{"modules/acme", "src/AuthBundle", "config"}
	for i, w := range wantDirs {
		if rc.Paths[i].Dir != w {
			t.Errorf("path[%d].Dir = %q; want %q (normalized, declaration order)", i, rc.Paths[i].Dir, w)
		}
	}
	// tri-state mentions
	if !rc.Paths[0].MentionsPresent || len(rc.Paths[0].Mentions) != 1 {
		t.Errorf("path[0] mentions = %+v present=%v; want one entry", rc.Paths[0].Mentions, rc.Paths[0].MentionsPresent)
	}
	if rc.Paths[1].Channel != "C0AUTH0000" {
		t.Errorf("path[1].Channel = %q; want C0AUTH0000", rc.Paths[1].Channel)
	}
	if !rc.Paths[2].MentionsPresent || len(rc.Paths[2].Mentions) != 0 {
		t.Errorf("path[2] mentions = %+v present=%v; want present+empty ([])", rc.Paths[2].Mentions, rc.Paths[2].MentionsPresent)
	}
}

func TestPaths_AbsentMentionsNotPresent(t *testing.T) {
	f, err := parseDoc(t, baseRepoHead+"      paths:\n        \"/legacy\": {channel: C0LEG00000}\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p := f.Mappings["acme"]["the-monorepo"].Paths[0]
	if p.MentionsPresent {
		t.Errorf("absent mentions should not be present (inherit): %+v", p)
	}
}

// H1 — duplicate key inside a tier (hand-rolled decoder previously last-wins).
func TestPaths_DuplicateChannelInTierRejected(t *testing.T) {
	_, err := parseDoc(t, "mappings:\n  acme:\n    the-monorepo:\n      channel: C0AAA00000\n      channel: C0BBB00000\n")
	if err == nil {
		t.Fatal("expected error for duplicate channel: in a tier")
	}
}

// H1 — duplicate key inside a path node.
func TestPaths_DuplicateKeyInPathNodeRejected(t *testing.T) {
	_, err := parseDoc(t, baseRepoHead+
		"      paths:\n        \"/src\": {channel: C0AAA00000, channel: C0BBB00000}\n")
	if err == nil {
		t.Fatal("expected error for duplicate key inside a path node")
	}
}

// H2 — two path keys that normalize to the same directory.
func TestPaths_NormalizationCollisionRejected(t *testing.T) {
	_, err := parseDoc(t, baseRepoHead+
		"      paths:\n        \"/config\": {channel: C0AAA00000}\n        \"config/\": {mentions: []}\n")
	if err == nil {
		t.Fatal("expected error for /config vs config/ collision")
	}
}

// L2 — empty/root/.. keys.
func TestPaths_RootKeyRejected(t *testing.T) {
	if _, err := parseDoc(t, baseRepoHead+"      paths:\n        \"/\": {channel: C0AAA00000}\n"); err == nil {
		t.Fatal("expected error for root path key")
	}
}

func TestPaths_DotDotKeyRejected(t *testing.T) {
	if _, err := parseDoc(t, baseRepoHead+"      paths:\n        \"/src/../etc\": {channel: C0AAA00000}\n"); err == nil {
		t.Fatal("expected error for .. in a path key")
	}
}

// M4 — paths on the "*" org-default tier.
func TestPaths_OnStarTierRejected(t *testing.T) {
	_, err := parseDoc(t, "mappings:\n  acme:\n    \"*\":\n      channel: C0STAR0000\n      paths:\n        \"/src\": {mentions: []}\n")
	if err == nil {
		t.Fatal("expected error for paths on the \"*\" tier")
	}
}

func TestPaths_InvalidChannelFormatRejected(t *testing.T) {
	_, err := parseDoc(t, baseRepoHead+"      paths:\n        \"/src\": {channel: \"not-a-channel\"}\n")
	if err == nil {
		t.Fatal("expected error for invalid path channel format")
	}
}

func TestPaths_UnknownKeyInPathRejected(t *testing.T) {
	_, err := parseDoc(t, baseRepoHead+"      paths:\n        \"/src\": {bogus: x}\n")
	if err == nil {
		t.Fatal("expected error for unknown key in a path node")
	}
}

func TestPaths_NullMentionsRejected(t *testing.T) {
	_, err := parseDoc(t, baseRepoHead+"      paths:\n        \"/src\": {mentions: null}\n")
	if err == nil {
		t.Fatal("expected error for mentions: null in a path node")
	}
}

// H3 — a repo tier with paths but no resolvable base channel.
func TestPaths_NoBaseChannelWithPathsRejected(t *testing.T) {
	_, err := parseDoc(t, "mappings:\n  acme:\n    the-monorepo:\n      paths:\n        \"/src\": {channel: C0AAA00000}\n")
	if err == nil {
		t.Fatal("expected error: repo has paths but no base channel and no \"*\"")
	}
}
