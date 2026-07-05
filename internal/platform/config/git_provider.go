package config

import (
	"fmt"

	"github.com/mptooling/notifycat/internal/kernel"
)

// Git provider tokens accepted by the required top-level git_provider key. Both
// are wired: a deployment serves exactly one, which selects the webhook route,
// the required webhook secret, and the validation/reconcile probes.
const (
	gitProviderGitHub    = kernel.ProviderGitHub    // "github"
	gitProviderBitbucket = kernel.ProviderBitbucket // "bitbucket"
)

// validateGitProvider enforces the required git_provider enum. An absent key
// (empty value) or an unknown token fails fast, naming the key, showing the
// valid values, and pointing at the upgrade doc.
func validateGitProvider(value kernel.Provider) error {
	switch value {
	case gitProviderGitHub, gitProviderBitbucket:
		return nil
	case "":
		return fmt.Errorf("config: required key git_provider is missing; add `git_provider: github` — see docs/upgrading.md")
	default:
		return fmt.Errorf("config: git_provider %q is invalid; valid values are `github` and `bitbucket` — see docs/upgrading.md", value)
	}
}
