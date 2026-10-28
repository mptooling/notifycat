package infrastructure

import (
	"regexp"
	"strings"

	"github.com/mptooling/notifycat/internal/feed/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

var wellFormedRepositoryName = regexp.MustCompile(domain.RepositoryNamePattern)

// RoutingCatalog answers which repositories the routing mappings configure.
type RoutingCatalog struct {
	repositories map[string]struct{}
	wildcardOrgs map[string]struct{}
}

var _ domain.RepositoryCatalog = (*RoutingCatalog)(nil)

// NewRoutingCatalog indexes the entries of the configured mappings.
func NewRoutingCatalog(entries routingdomain.EntryLister) *RoutingCatalog {
	catalog := &RoutingCatalog{
		repositories: map[string]struct{}{},
		wildcardOrgs: map[string]struct{}{},
	}
	for _, entry := range entries.Entries() {
		org := strings.ToLower(entry.Org)
		if entry.Wildcard {
			catalog.wildcardOrgs[org] = struct{}{}
			continue
		}
		catalog.repositories[org+"/"+strings.ToLower(entry.Repo)] = struct{}{}
	}
	return catalog
}

// IsConfigured implements domain.RepositoryCatalog.
func (catalog *RoutingCatalog) IsConfigured(repository string) bool {
	name := strings.ToLower(repository)
	if !wellFormedRepositoryName.MatchString(name) {
		return false
	}
	org, _, _ := strings.Cut(name, "/")
	if _, explicit := catalog.repositories[name]; explicit {
		return true
	}
	_, wildcard := catalog.wildcardOrgs[org]
	return wildcard
}

// Configured implements domain.RepositoryCatalog.
func (catalog *RoutingCatalog) Configured(repositories []string) []string {
	seen := make(map[string]struct{}, len(repositories))
	configured := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		name := strings.ToLower(repository)
		if _, duplicate := seen[name]; duplicate || !catalog.IsConfigured(name) {
			continue
		}
		seen[name] = struct{}{}
		configured = append(configured, name)
	}
	return configured
}
