package pullrequest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mptooling/notifycat/internal/pullrequest"
	"github.com/mptooling/notifycat/internal/store"
)

// stubMappings implements pullrequest.PathMappings for testing.
type stubMappings struct {
	base         store.RepoMapping
	baseErr      error
	targets      []store.Target
	hasPathRules bool
}

func (s *stubMappings) Get(_ context.Context, repository string) (store.RepoMapping, error) {
	if s.baseErr != nil {
		return store.RepoMapping{}, s.baseErr
	}
	m := s.base
	m.Repository = repository
	return m, nil
}

func (s *stubMappings) RepoHasPathRules(string) bool { return s.hasPathRules }

func (s *stubMappings) TargetsForFiles(string, []string) []store.Target { return s.targets }

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

func TestRouter_NoFetcherReturnsBaseTarget(t *testing.T) {
	m := &stubMappings{base: store.RepoMapping{SlackChannel: "C0BASE", Mentions: []string{"<!here>"}}, hasPathRules: true}
	r := pullrequest.NewRouter(m, nil, discardLogger())
	_, targets, err := r.ResolveTargets(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(targets) != 1 || targets[0].Channel != "C0BASE" {
		t.Fatalf("want single base target; got %+v", targets)
	}
}

func TestRouter_FanOutTargets(t *testing.T) {
	m := &stubMappings{
		base:         store.RepoMapping{SlackChannel: "C0BASE"},
		hasPathRules: true,
		targets:      []store.Target{{Channel: "C0A"}, {Channel: "C0B"}},
	}
	files := &stubFiles{files: []string{"a", "b"}}
	r := pullrequest.NewRouter(m, files, discardLogger())
	_, targets, err := r.ResolveTargets(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(targets) != 2 || files.calls != 1 {
		t.Fatalf("want 2 targets from one fetch; got %d targets, %d calls", len(targets), files.calls)
	}
}

func TestRouter_FetchErrorFallsBackToBase(t *testing.T) {
	m := &stubMappings{base: store.RepoMapping{SlackChannel: "C0BASE"}, hasPathRules: true, targets: []store.Target{{Channel: "C0A"}}}
	files := &stubFiles{err: errors.New("github down")}
	r := pullrequest.NewRouter(m, files, discardLogger())
	_, targets, err := r.ResolveTargets(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("should soft-fail: %v", err)
	}
	if len(targets) != 1 || targets[0].Channel != "C0BASE" {
		t.Fatalf("fetch error should fall back to base; got %+v", targets)
	}
}
