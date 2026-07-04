package infrastructure

import (
	"context"
	"fmt"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/store"
)

// StoreSmokeMessages implements diagnosticsdomain.SmokeMessages over
// *store.PullRequests, converting store.Message to domain.SmokeMessage.
type StoreSmokeMessages struct {
	pullRequests *store.PullRequests
}

// NewStoreSmokeMessages returns a StoreSmokeMessages backed by the given store.
func NewStoreSmokeMessages(pullRequests *store.PullRequests) *StoreSmokeMessages {
	return &StoreSmokeMessages{pullRequests: pullRequests}
}

// Messages returns the PR's stored messages as domain SmokeMessages. A
// store.ErrNotFound is propagated as-is so the application layer can detect
// the "no row" case.
func (s *StoreSmokeMessages) Messages(ctx context.Context, repository string, prNumber int) ([]diagnosticsdomain.SmokeMessage, error) {
	storeMessages, err := s.pullRequests.Messages(ctx, repository, prNumber)
	if err != nil {
		return nil, err
	}
	result := make([]diagnosticsdomain.SmokeMessage, len(storeMessages))
	for i, m := range storeMessages {
		result[i] = diagnosticsdomain.SmokeMessage{
			Channel:   m.Channel,
			MessageID: m.MessageID,
		}
	}
	return result, nil
}

// StoreSmokeCleanup implements diagnosticsdomain.SmokeCleanup over
// *store.PullRequests.
type StoreSmokeCleanup struct {
	pullRequests *store.PullRequests
}

// NewStoreSmokeCleanup returns a StoreSmokeCleanup backed by the given store.
func NewStoreSmokeCleanup(pullRequests *store.PullRequests) *StoreSmokeCleanup {
	return &StoreSmokeCleanup{pullRequests: pullRequests}
}

// DeletePR deletes the synthetic pull_requests row. It is a no-op when the row
// is absent, so it is safe to call even if delivery failed.
func (s *StoreSmokeCleanup) DeletePR(ctx context.Context, repository string, prNumber int) error {
	return s.pullRequests.Delete(ctx, repository, prNumber)
}

// MappingsSmokeMappings implements diagnosticsdomain.SmokeMappings over a
// mappings provider that satisfies the routing domain's MappingProvider port.
type MappingsSmokeMappings struct {
	provider smokeRoutingProvider
}

// smokeRoutingProvider is the slice of the mappings provider the smoke adapter
// needs: resolving a repository to its RepoMapping.
type smokeRoutingProvider interface {
	Get(ctx context.Context, repository string) (routingdomain.RepoMapping, error)
}

// NewMappingsSmokeMappings returns a SmokeMappings adapter for any provider
// that can resolve a repository to a RepoMapping.
func NewMappingsSmokeMappings(provider smokeRoutingProvider) *MappingsSmokeMappings {
	return &MappingsSmokeMappings{provider: provider}
}

// Get resolves target to its RepoMapping. Returns routingdomain.ErrNotFound
// (which is identical to store.ErrNotFound) when the repository is absent.
func (m *MappingsSmokeMappings) Get(ctx context.Context, repository string) (routingdomain.RepoMapping, error) {
	mapping, err := m.provider.Get(ctx, repository)
	if err != nil {
		return routingdomain.RepoMapping{}, fmt.Errorf("smoke mappings: %w", err)
	}
	return mapping, nil
}
