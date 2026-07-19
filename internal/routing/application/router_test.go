package application_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"

	application "github.com/mptooling/notifycat/internal/routing/application"
	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

// stubMappings implements domain.RoutingProvider for testing.
type stubMappings struct {
	base         domain.RepoMapping
	baseErr      error
	targets      []domain.Target
	hasPathRules bool
}

func (s *stubMappings) Get(_ context.Context, repository string) (domain.RepoMapping, error) {
	if s.baseErr != nil {
		return domain.RepoMapping{}, s.baseErr
	}
	m := s.base
	m.Repository = repository
	return m, nil
}

func (s *stubMappings) RepoHasPathRules(string) bool { return s.hasPathRules }

func (s *stubMappings) TargetsForFiles(string, []string) []domain.Target { return s.targets }

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
	m := &stubMappings{base: domain.RepoMapping{SlackChannel: "C0BASE", Mentions: []string{"<!here>"}}, hasPathRules: true}
	r := application.NewRouter(m, nil, discardLogger())
	resolved, err := r.ResolveTargets(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved.Targets) != 1 || resolved.Targets[0].Channel != "C0BASE" {
		t.Fatalf("want single base target; got %+v", resolved.Targets)
	}
}

func TestRouter_FanOutTargets(t *testing.T) {
	m := &stubMappings{
		base:         domain.RepoMapping{SlackChannel: "C0BASE"},
		hasPathRules: true,
		targets:      []domain.Target{{Channel: "C0A"}, {Channel: "C0B"}},
	}
	files := &stubFiles{files: []string{"a", "b"}}
	r := application.NewRouter(m, files, discardLogger())
	resolved, err := r.ResolveTargets(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved.Targets) != 2 || files.calls != 1 {
		t.Fatalf("want 2 targets from one fetch; got %d targets, %d calls", len(resolved.Targets), files.calls)
	}
}

func TestRouter_FetchErrorFallsBackToBase(t *testing.T) {
	m := &stubMappings{base: domain.RepoMapping{SlackChannel: "C0BASE"}, hasPathRules: true, targets: []domain.Target{{Channel: "C0A"}}}
	files := &stubFiles{err: errors.New("github down")}
	r := application.NewRouter(m, files, discardLogger())
	resolved, err := r.ResolveTargets(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("should soft-fail: %v", err)
	}
	if len(resolved.Targets) != 1 || resolved.Targets[0].Channel != "C0BASE" {
		t.Fatalf("fetch error should fall back to base; got %+v", resolved.Targets)
	}
}

func TestRouter_ResolvedTargetsCarryChangedFiles(t *testing.T) {
	m := &stubMappings{
		base:         domain.RepoMapping{SlackChannel: "C0BASE"},
		hasPathRules: true,
		targets:      []domain.Target{{Channel: "C0A"}},
	}
	files := &stubFiles{files: []string{"services/payments/main.go", "docs/readme.md"}}
	r := application.NewRouter(m, files, discardLogger())

	resolved, err := r.ResolveTargets(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !reflect.DeepEqual(resolved.ChangedFiles, files.files) {
		t.Errorf("ChangedFiles = %v; want the fetched list", resolved.ChangedFiles)
	}
	if resolved.Mapping.Repository != "acme/mono" {
		t.Errorf("Mapping.Repository = %q", resolved.Mapping.Repository)
	}
	if len(resolved.Targets) != 1 || resolved.Targets[0].Channel != "C0A" {
		t.Errorf("Targets = %+v", resolved.Targets)
	}
}

func TestRouter_NoFetcherHasNoChangedFiles(t *testing.T) {
	m := &stubMappings{base: domain.RepoMapping{SlackChannel: "C0BASE"}, hasPathRules: true}
	r := application.NewRouter(m, nil, discardLogger())

	resolved, err := r.ResolveTargets(context.Background(), "acme/mono", 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.ChangedFiles != nil {
		t.Errorf("ChangedFiles = %v; want nil without a fetcher", resolved.ChangedFiles)
	}
}
