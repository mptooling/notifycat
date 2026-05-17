package mappings

import "testing"

func TestEntry_Hash_StableAcrossMentionReorder(t *testing.T) {
	a := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{"@x", "@y"}}
	b := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{"@y", "@x"}}
	if a.Hash() != b.Hash() {
		t.Errorf("hash should be stable across mention reorder: %s vs %s", a.Hash(), b.Hash())
	}
}

func TestEntry_Hash_DiffersOnChannel(t *testing.T) {
	a := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}}
	b := Entry{Org: "acme", Repo: "api", Channel: "C2", Mentions: []string{}}
	if a.Hash() == b.Hash() {
		t.Errorf("hash must differ across channel change")
	}
}

func TestEntry_Hash_DiffersOnWildcardVsExplicit(t *testing.T) {
	a := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}}
	b := Entry{Org: "acme", Wildcard: true, Channel: "C1", Mentions: []string{}}
	if a.Hash() == b.Hash() {
		t.Errorf("wildcard hash must differ from explicit hash")
	}
}

func TestEntry_Hash_DiffersOnMentions(t *testing.T) {
	a := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{"@x"}}
	b := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{"@x", "@y"}}
	if a.Hash() == b.Hash() {
		t.Errorf("hash must differ when mentions change")
	}
}
