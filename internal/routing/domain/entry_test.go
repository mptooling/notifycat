package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/kernel"
)

func TestEntry_Hash_IgnoresMentions(t *testing.T) {
	withTwoMentions := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{"@x", "@y"}}
	withoutMentions := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: nil}
	withOtherMention := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{"@z"}}

	assert.Equal(t, withoutMentions.Hash(), withTwoMentions.Hash())
	assert.Equal(t, withoutMentions.Hash(), withOtherMention.Hash())
}

func TestEntry_Hash_DiffersOnChannel(t *testing.T) {
	first := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}}
	second := Entry{Org: "acme", Repo: "api", Channel: "C2", Mentions: []string{}}

	assert.NotEqual(t, first.Hash(), second.Hash())
}

func TestEntry_Hash_DiffersOnWildcardVsExplicit(t *testing.T) {
	explicit := Entry{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}}
	wildcard := Entry{Org: "acme", Wildcard: true, Channel: "C1", Mentions: []string{}}

	assert.NotEqual(t, explicit.Hash(), wildcard.Hash())
}

func TestEntry_Hash_DiffersOnProvider(t *testing.T) {
	github := Entry{Org: "acme", Repo: "api", Channel: "C1", Provider: "github"}
	bitbucket := Entry{Org: "acme", Repo: "api", Channel: "C1", Provider: "bitbucket"}

	assert.NotEqual(t, github.Hash(), bitbucket.Hash(), "flipping the provider must revalidate the whole lock")
}

func TestEntry_Hash_DiffersOnPathChannels(t *testing.T) {
	noExtras := Entry{Org: "acme", Repo: "api", Channel: "C1"}
	oneExtra := Entry{Org: "acme", Repo: "api", Channel: "C1", ExtraChannels: []string{"C2"}}
	otherExtra := Entry{Org: "acme", Repo: "api", Channel: "C1", ExtraChannels: []string{"C3"}}

	assert.NotEqual(t, noExtras.Hash(), oneExtra.Hash(), "adding a path channel must re-run validation")
	assert.NotEqual(t, oneExtra.Hash(), otherExtra.Hash(), "repointing a path channel must re-run validation")
}

func TestEntryHash_StableForSingleChannel(t *testing.T) {
	// Golden hash: a single-channel entry (no channels: list, empty ExtraChannels)
	// must hash to this exact value so existing config.lock entries are not
	// invalidated on upgrade. If this breaks, the hash payload changed in a
	// backward-incompatible way.
	const want = "bbb33b7da026b37fbc51e7b0dc0a47b88ff942f0801e4aad792320c55f08dfba"

	got := Entry{Org: "acme", Repo: "api", Channel: "C0API", Provider: kernel.ProviderGitHub}.Hash()

	assert.Equal(t, want, got)
}

func TestEntryHash_ChangesWhenExtraChannelAdded(t *testing.T) {
	before := Entry{Org: "acme", Repo: "api", Channel: "C0API", Provider: kernel.ProviderGitHub}
	after := Entry{Org: "acme", Repo: "api", Channel: "C0API", ExtraChannels: []string{"C0API2"}, Provider: kernel.ProviderGitHub}

	assert.NotEqual(t, before.Hash(), after.Hash())
}
