package application_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	application "github.com/mptooling/notifycat/internal/routing/application"
	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

// stubMappings implements domain.RoutingProvider for testing.
type stubMappings struct {
	base         domain.RepoMapping
	baseErr      error
	targets      []domain.Target
	baseTargets  []domain.Target
	hasPathRules bool
}

func (s *stubMappings) Get(_ context.Context, repository string) (domain.RepoMapping, error) {
	if s.baseErr != nil {
		return domain.RepoMapping{}, s.baseErr
	}
	mapping := s.base
	mapping.Repository = repository
	return mapping, nil
}

func (s *stubMappings) RepoHasPathRules(string) bool { return s.hasPathRules }

func (s *stubMappings) TargetsForFiles(string, []string) []domain.Target { return s.targets }

func (s *stubMappings) BaseTargets(string) []domain.Target { return s.baseTargets }

type stubFiles struct {
	files []string
	err   error
	calls int
}

func (s *stubFiles) ListPullRequestFiles(_ context.Context, _, _ string, _ int) ([]string, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.files, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRouter_NoFetcherReturnsBaseTarget(t *testing.T) {
	mappings := &stubMappings{
		base:         domain.RepoMapping{SlackChannel: "C0OLDUNUSED", Mentions: []string{"<!here>"}},
		baseTargets:  []domain.Target{{Channel: "C0BASE", Mentions: []string{"<!here>"}}},
		hasPathRules: true,
	}
	router := application.NewRouter(mappings, nil, discardLogger())

	_, targets, err := router.ResolveTargets(context.Background(), "acme/mono", 7)

	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, "C0BASE", targets[0].Channel)
}

func TestRouter_FanOutTargets(t *testing.T) {
	mappings := &stubMappings{
		base:         domain.RepoMapping{SlackChannel: "C0BASE"},
		hasPathRules: true,
		targets:      []domain.Target{{Channel: "C0A"}, {Channel: "C0B"}},
	}
	files := &stubFiles{files: []string{"a", "b"}}
	router := application.NewRouter(mappings, files, discardLogger())

	_, targets, err := router.ResolveTargets(context.Background(), "acme/mono", 7)

	require.NoError(t, err)
	assert.Len(t, targets, 2)
	assert.Equal(t, 1, files.calls, "the changed-file list is fetched once per event")
}

func TestRouter_FetchErrorFallsBackToBase(t *testing.T) {
	mappings := &stubMappings{
		base:         domain.RepoMapping{SlackChannel: "C0BASE"},
		baseTargets:  []domain.Target{{Channel: "C0BASE"}},
		hasPathRules: true,
		targets:      []domain.Target{{Channel: "C0A"}},
	}
	files := &stubFiles{err: errors.New("github down")}
	router := application.NewRouter(mappings, files, discardLogger())

	_, targets, err := router.ResolveTargets(context.Background(), "acme/mono", 7)

	require.NoError(t, err, "a failed file listing must soft-fail")
	require.Len(t, targets, 1)
	assert.Equal(t, "C0BASE", targets[0].Channel)
}

func TestResolveTargets_NoPathRulesReturnsFullBaseSet(t *testing.T) {
	mappings := &stubMappings{
		base:         domain.RepoMapping{SlackChannel: "C0B1"},
		baseTargets:  []domain.Target{{Channel: "C0B1"}, {Channel: "C0B2"}},
		hasPathRules: false,
	}
	router := application.NewRouter(mappings, nil, discardLogger())

	_, targets, err := router.ResolveTargets(context.Background(), "acme/api", 7)

	require.NoError(t, err)
	require.Len(t, targets, 2)
	assert.Equal(t, "C0B1", targets[0].Channel)
	assert.Equal(t, "C0B2", targets[1].Channel)
}
