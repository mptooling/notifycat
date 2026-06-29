package pullrequest_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/mptooling/notifycat/internal/pullrequest"
	"github.com/mptooling/notifycat/internal/store"
)

// stubMappings implements pullrequest.PathMappings. It records whether the
// file-aware path was taken so tests can assert routing decisions.
type stubMappings struct {
	base         store.RepoMapping
	baseErr      error
	pathResult   store.RepoMapping
	hasPathRules bool
	gotFiles     []string
	getForCalled bool
}

func (s *stubMappings) Get(_ context.Context, repository string) (store.RepoMapping, error) {
	if s.baseErr != nil {
		return store.RepoMapping{}, s.baseErr
	}
	m := s.base
	m.Repository = repository
	return m, nil
}

func (s *stubMappings) GetForFiles(_ context.Context, _ *slog.Logger, repository string, files []string) (store.RepoMapping, error) {
	s.getForCalled = true
	s.gotFiles = files
	m := s.pathResult
	m.Repository = repository
	return m, nil
}

func (s *stubMappings) RepoHasPathRules(string) bool { return s.hasPathRules }

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

func TestRouter_NoFetcherUsesBaseTier(t *testing.T) {
	m := &stubMappings{base: store.RepoMapping{SlackChannel: "C0BASE"}, hasPathRules: true}
	r := pullrequest.NewRouter(m, nil, discardLogger())

	got, err := r.Resolve(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.SlackChannel != "C0BASE" || m.getForCalled {
		t.Errorf("no fetcher should resolve to base tier without path resolution; got %+v getForCalled=%v", got, m.getForCalled)
	}
}

func TestRouter_NoPathRulesSkipsFetch(t *testing.T) {
	m := &stubMappings{base: store.RepoMapping{SlackChannel: "C0BASE"}, hasPathRules: false}
	files := &stubFiles{files: []string{"x.go"}}
	r := pullrequest.NewRouter(m, files, discardLogger())

	got, err := r.Resolve(context.Background(), "acme/plain", 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.SlackChannel != "C0BASE" || files.calls != 0 {
		t.Errorf("repo without paths should not fetch files; got %+v fetchCalls=%d", got, files.calls)
	}
}

func TestRouter_PathRulesFetchAndResolve(t *testing.T) {
	m := &stubMappings{
		base:         store.RepoMapping{SlackChannel: "C0BASE"},
		pathResult:   store.RepoMapping{SlackChannel: "C0PATH"},
		hasPathRules: true,
	}
	files := &stubFiles{files: []string{"modules/acme/x.go"}}
	r := pullrequest.NewRouter(m, files, discardLogger())

	got, err := r.Resolve(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.SlackChannel != "C0PATH" {
		t.Errorf("channel = %q; want path-resolved C0PATH", got.SlackChannel)
	}
	if files.calls != 1 || !m.getForCalled {
		t.Errorf("expected one fetch + GetForFiles; fetchCalls=%d getForCalled=%v", files.calls, m.getForCalled)
	}
	if len(m.gotFiles) != 1 || m.gotFiles[0] != "modules/acme/x.go" {
		t.Errorf("GetForFiles got files %v; want [modules/acme/x.go]", m.gotFiles)
	}
}

func TestRouter_FetchErrorFallsBackToBase(t *testing.T) {
	m := &stubMappings{
		base:         store.RepoMapping{SlackChannel: "C0BASE"},
		pathResult:   store.RepoMapping{SlackChannel: "C0PATH"},
		hasPathRules: true,
	}
	files := &stubFiles{err: errors.New("github down")}
	r := pullrequest.NewRouter(m, files, discardLogger())

	got, err := r.Resolve(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("resolve should soft-fail, not error: %v", err)
	}
	if got.SlackChannel != "C0BASE" || m.getForCalled {
		t.Errorf("fetch error should fall back to base tier; got %+v getForCalled=%v", got, m.getForCalled)
	}
}

func TestRouter_MalformedRepositoryFallsBack(t *testing.T) {
	m := &stubMappings{base: store.RepoMapping{SlackChannel: "C0BASE"}, hasPathRules: true}
	files := &stubFiles{files: []string{"x.go"}}
	r := pullrequest.NewRouter(m, files, discardLogger())

	got, err := r.Resolve(context.Background(), "no-slash", 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.SlackChannel != "C0BASE" || files.calls != 0 {
		t.Errorf("malformed repository should fall back without fetching; got %+v fetchCalls=%d", got, files.calls)
	}
}

func TestRouter_PropagatesNotFound(t *testing.T) {
	m := &stubMappings{baseErr: store.ErrNotFound, hasPathRules: false}
	r := pullrequest.NewRouter(m, nil, discardLogger())

	if _, err := r.Resolve(context.Background(), "acme/unmapped", 7); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound; got %v", err)
	}
}
