package application_test

import (
	"context"
	"errors"
	"testing"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/validation/application"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

type stubLister struct {
	repos []string
	err   error
}

func (s *stubLister) ListOrgRepos(_ context.Context, _ string) ([]string, error) {
	return s.repos, s.err
}

type stubValidator struct {
	calls []string
	err   func(string) bool
}

func (s *stubValidator) Validate(_ context.Context, repository string) domain.Report {
	s.calls = append(s.calls, repository)
	if s.err != nil && s.err(repository) {
		return domain.Report{Repository: repository, Checks: []domain.CheckResult{{Name: "x", Status: domain.StatusFail, Detail: "boom"}}}
	}
	return domain.Report{Repository: repository, Checks: []domain.CheckResult{{Name: "x", Status: domain.StatusOK, Detail: "ok"}}}
}

func TestRunForEntries_ExplicitOnly(t *testing.T) {
	entries := []routingdomain.Entry{
		{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}},
		{Org: "acme", Repo: "web", Channel: "C1", Mentions: []string{}},
	}
	sv := &stubValidator{}
	results := application.RunForEntries(context.Background(), entries, nil, sv)
	if len(results) != 2 || len(results[0].Reports) != 1 || len(results[1].Reports) != 1 {
		t.Fatalf("results=%d reports=%d/%d; want 2/1/1", len(results), len(results[0].Reports), len(results[1].Reports))
	}
	if sv.calls[0] != "acme/api" || sv.calls[1] != "acme/web" {
		t.Errorf("calls = %v", sv.calls)
	}
	if !results[0].OK() || !results[1].OK() {
		t.Errorf("expected both OK; got %+v", results)
	}
}

func TestRunForEntries_WildcardExpansion(t *testing.T) {
	entries := []routingdomain.Entry{{Org: "beta", Wildcard: true, Channel: "C2", Mentions: []string{}}}
	lister := &stubLister{repos: []string{"r1", "r2", "r3"}}
	sv := &stubValidator{}
	results := application.RunForEntries(context.Background(), entries, lister, sv)
	if len(results) != 1 || len(results[0].Reports) != 3 {
		t.Fatalf("results=%d reports[0]=%d; want 1/3", len(results), len(results[0].Reports))
	}
	want := []string{"beta/r1", "beta/r2", "beta/r3"}
	for i, w := range want {
		if sv.calls[i] != w {
			t.Errorf("call[%d] = %q; want %q", i, sv.calls[i], w)
		}
	}
	if !results[0].OK() {
		t.Errorf("expected OK on full expansion; got %+v", results[0])
	}
}

func TestRunForEntries_WildcardWithoutLister_SkipsButReports(t *testing.T) {
	entries := []routingdomain.Entry{{Org: "beta", Wildcard: true, Channel: "C2", Mentions: []string{}}}
	results := application.RunForEntries(context.Background(), entries, nil, &stubValidator{})
	if len(results) != 1 || len(results[0].Reports) != 1 {
		t.Fatalf("results=%d reports=%d; want 1/1", len(results), len(results[0].Reports))
	}
	r := results[0].Reports[0]
	if r.Repository != "beta/*" || r.Checks[0].Status != domain.StatusSkip {
		t.Errorf("expected single skip on beta/*; got %+v", r)
	}
	if !results[0].OK() {
		t.Errorf("a skip is not a failure; OK() should be true; got %+v", results[0])
	}
}

func TestRunForEntries_ListerError_BecomesFailingEntryAndContinues(t *testing.T) {
	entries := []routingdomain.Entry{
		{Org: "beta", Wildcard: true, Channel: "C2", Mentions: []string{}},
		{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}},
	}
	lister := &stubLister{err: errors.New("rate-limited")}
	sv := &stubValidator{}
	results := application.RunForEntries(context.Background(), entries, lister, sv)
	if len(results) != 2 {
		t.Fatalf("results = %d; want 2", len(results))
	}
	if results[0].OK() || results[0].Reports[0].Repository != "beta/*" {
		t.Errorf("first result should be failing beta/*; got %+v", results[0])
	}
	if !results[1].OK() || results[1].Reports[0].Repository != "acme/api" {
		t.Errorf("second result should be OK acme/api; got %+v", results[1])
	}
}

func TestRunForEntries_PerRepoFailureDoesNotAbort(t *testing.T) {
	entries := []routingdomain.Entry{
		{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}},
		{Org: "acme", Repo: "web", Channel: "C1", Mentions: []string{}},
	}
	sv := &stubValidator{err: func(r string) bool { return r == "acme/api" }}
	results := application.RunForEntries(context.Background(), entries, nil, sv)
	if len(results) != 2 {
		t.Fatalf("results = %d; want 2", len(results))
	}
	if results[0].OK() || !results[1].OK() {
		t.Errorf("expected first fail, second ok: %+v", results)
	}
}
