package config

import (
	"fmt"

	"github.com/mptooling/notifycat/internal/kernel"
)

// Git provider tokens accepted by the required top-level git_provider key. Only
// github is wired in this release; bitbucket is a recognized-but-not-yet-wired
// value so a config that reaches for it gets a "coming soon" message rather than
// a generic "invalid" one. The Bitbucket inbound-stack slice flips it on.
const (
	gitProviderGitHub    = kernel.ProviderGitHub        // "github"
	gitProviderBitbucket = kernel.Provider("bitbucket") // recognized, not yet wired
)

// validateGitProvider enforces the required git_provider enum. An absent key
// (empty value) or an unknown token fails fast, naming the key, showing the one
// valid line, and pointing at the upgrade doc; bitbucket gets its own
// not-yet-supported message so operators know the flip is coming, not a typo.
func validateGitProvider(value kernel.Provider) error {
	switch value {
	case gitProviderGitHub:
		return nil
	case "":
		return fmt.Errorf("config: required key git_provider is missing; add `git_provider: github` — see docs/upgrading.md")
	case gitProviderBitbucket:
		return fmt.Errorf("config: git_provider %q is not yet supported; use `git_provider: github` — see docs/upgrading.md", value)
	default:
		return fmt.Errorf("config: git_provider %q is invalid; the only valid value is `git_provider: github` — see docs/upgrading.md", value)
	}
}
