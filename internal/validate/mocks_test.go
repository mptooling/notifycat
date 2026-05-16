package validate_test

import (
	"context"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// mockMappingLookup is a hand-written test double for validate.MappingLookup.
// Each method is wired through a function value so individual tests can
// install behavior per-case without subclassing.
type mockMappingLookup struct {
	get  func(ctx context.Context, repository string) (store.RepoMapping, error)
	list func(ctx context.Context) ([]store.RepoMapping, error)
}

func (m *mockMappingLookup) Get(ctx context.Context, repository string) (store.RepoMapping, error) {
	return m.get(ctx, repository)
}

func (m *mockMappingLookup) List(ctx context.Context) ([]store.RepoMapping, error) {
	return m.list(ctx)
}

type mockSlackChecker struct {
	authTest          func(ctx context.Context) (string, []string, error)
	conversationsInfo func(ctx context.Context, channel string) (slack.ChannelInfo, error)
}

func (m *mockSlackChecker) AuthTest(ctx context.Context) (string, []string, error) {
	return m.authTest(ctx)
}

func (m *mockSlackChecker) ConversationsInfo(ctx context.Context, channel string) (slack.ChannelInfo, error) {
	return m.conversationsInfo(ctx, channel)
}

type mockGitHubChecker struct {
	listHookEvents func(ctx context.Context, owner, repo, urlSuffix string) ([]string, error)
}

func (m *mockGitHubChecker) ListHookEvents(ctx context.Context, owner, repo, urlSuffix string) ([]string, error) {
	return m.listHookEvents(ctx, owner, repo, urlSuffix)
}
