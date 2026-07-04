package doctor

import (
	"fmt"

	routingapp "github.com/mptooling/notifycat/internal/routing/application"
)

// CheckMappings reports whether the `mappings:` section of config.yaml parsed
// into any entries. An empty section is OK (the server boots but routes
// nothing). Parse failures already surface in config load, so by the time the
// doctor has a provider the file is structurally valid.
//
// When any tier configures per-path routing, it adds a "path routing" check:
// OK when a GitHub token is present (paths are active), SKIP when it is absent
// (path rules are inert — PRs route to the repo tier — until a token is set).
func CheckMappings(provider *routingapp.Provider, hasGitHubToken bool) Section {
	sec := Section{Name: "mappings"}
	entries := provider.Entries()
	if len(entries) == 0 {
		sec.Checks = append(sec.Checks, okResult("entries", "0 entries (server will boot but route nothing)"))
		return sec
	}
	sec.Checks = append(sec.Checks, okResult("entries", fmt.Sprintf("%d entries", len(entries))))
	if provider.HasPathRules() {
		if hasGitHubToken {
			sec.Checks = append(sec.Checks, okResult("path routing", "active (GITHUB_TOKEN set)"))
		} else {
			sec.Checks = append(sec.Checks, skip("path routing",
				"GITHUB_TOKEN unset; path rules are inert — PRs route to the repo tier until a token is set"))
		}
	}
	return sec
}
