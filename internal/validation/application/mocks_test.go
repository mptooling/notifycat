package application_test

import (
	"context"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

// mockMappingLookup is a hand-written test double for domain.MappingLookup.
// Each method is wired through a function value so individual tests can install
// behavior per-case without subclassing.
type mockMappingLookup struct {
	get          func(ctx context.Context, repository string) (routingdomain.RepoMapping, error)
	pathChannels func(repository string) []string
}

func (m *mockMappingLookup) Get(ctx context.Context, repository string) (routingdomain.RepoMapping, error) {
	return m.get(ctx, repository)
}

func (m *mockMappingLookup) PathChannels(repository string) []string {
	if m.pathChannels == nil {
		return nil
	}
	return m.pathChannels(repository)
}

type mockSlackChecker struct {
	authTest          func(ctx context.Context) (string, []string, error)
	conversationsInfo func(ctx context.Context, channel string) (domain.ChannelInfo, error)
}

func (m *mockSlackChecker) AuthTest(ctx context.Context) (string, []string, error) {
	return m.authTest(ctx)
}

func (m *mockSlackChecker) ConversationsInfo(ctx context.Context, channel string) (domain.ChannelInfo, error) {
	return m.conversationsInfo(ctx, channel)
}

type mockHookChecker struct {
	listHookEvents func(ctx context.Context, owner, repo, urlSuffix string) ([]string, error)
}

func (m *mockHookChecker) ListHookEvents(ctx context.Context, owner, repo, urlSuffix string) ([]string, error) {
	return m.listHookEvents(ctx, owner, repo, urlSuffix)
}
