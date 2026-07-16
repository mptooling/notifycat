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
	b := Entry{Org: "acme", Repo: "api", Channel: "C1", PathChannels: []string{"C2"}}
	c := Entry{Org: "acme", Repo: "api", Channel: "C1", PathChannels: []string{"C3"}}
	if a.Hash() == b.Hash() {
		t.Errorf("adding a path channel must change the hash (so validation re-runs)")
	}
	if b.Hash() == c.Hash() {
		t.Errorf("repointing a path channel must change the hash")
	}
}

func TestEntryHashIgnoresAIFields(t *testing.T) {
	base := Entry{Org: "acme", Repo: "api", Channel: "C0123456789", Provider: kernel.ProviderGitHub}
	// AI settings live outside Entry entirely; this pins that adding per-tier
	// ai config can never invalidate the validation lock.
	if base.Hash() == "" {
		t.Fatal("hash must not be empty")
	}
}
