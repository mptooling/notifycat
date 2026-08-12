package domain

import (
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
)

func TestEntry_Hash_IgnoresMentions(t *testing.T) {
	a := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{"@x", "@y"}}
	b := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: nil}
	c := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{"@z"}}
	if a.Hash() != b.Hash() || a.Hash() != c.Hash() {
		t.Errorf("mentions must not affect hash: %s / %s / %s", a.Hash(), b.Hash(), c.Hash())
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

func TestEntry_Hash_DiffersOnProvider(t *testing.T) {
	github := Entry{Org: "acme", Repo: "api", Channel: "C1", Provider: "github"}
	bitbucket := Entry{Org: "acme", Repo: "api", Channel: "C1", Provider: "bitbucket"}
	if github.Hash() == bitbucket.Hash() {
		t.Errorf("flipping the provider must change the hash (so the whole lock revalidates)")
	}
}

func TestEntry_Hash_DiffersOnPathChannels(t *testing.T) {
	a := Entry{Org: "acme", Repo: "api", Channel: "C1"}
	b := Entry{Org: "acme", Repo: "api", Channel: "C1", ExtraChannels: []string{"C2"}}
	c := Entry{Org: "acme", Repo: "api", Channel: "C1", ExtraChannels: []string{"C3"}}
	if a.Hash() == b.Hash() {
		t.Errorf("adding a path channel must change the hash (so validation re-runs)")
	}
	if b.Hash() == c.Hash() {
		t.Errorf("repointing a path channel must change the hash")
	}
}

func TestEntryHash_StableForSingleChannel(t *testing.T) {
	// Golden hash: a single-channel entry (no channels: list, empty ExtraChannels)
	// must hash to this exact value so existing config.lock entries are not
	// invalidated on upgrade. If this breaks, the hash payload changed in a
	// backward-incompatible way.
	const want = "bbb33b7da026b37fbc51e7b0dc0a47b88ff942f0801e4aad792320c55f08dfba"
	got := Entry{Org: "acme", Repo: "api", Channel: "C0API", Provider: kernel.ProviderGitHub}.Hash()
	if got != want {
		t.Fatalf("single-channel entry hash changed (backward-incompatible lock): got %s want %s", got, want)
	}
}

func TestEntryHash_ChangesWhenExtraChannelAdded(t *testing.T) {
	before := Entry{Org: "acme", Repo: "api", Channel: "C0API", Provider: kernel.ProviderGitHub}
	after := Entry{Org: "acme", Repo: "api", Channel: "C0API", ExtraChannels: []string{"C0API2"}, Provider: kernel.ProviderGitHub}
	if before.Hash() == after.Hash() {
		t.Fatal("adding a channels: list must change the entry hash")
	}
}
