package validate

import (
	"context"
	"errors"
	"testing"

	"github.com/mptooling/notifycat/internal/mappings"
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

func (s *stubValidator) Validate(_ context.Context, repository string) Report {
	s.calls = append(s.calls, repository)
	if s.err != nil && s.err(repository) {
		return Report{Repository: repository, Checks: []CheckResult{{Name: "x", Status: StatusFail, Detail: "boom"}}}
	}
	return Report{Repository: repository, Checks: []CheckResult{{Name: "x", Status: StatusOK, Detail: "ok"}}}
}

func TestRunForEntries_ExplicitOnly(t *testing.T) {
	entries := []mappings.Entry{
		{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}},
		{Org: "acme", Repo: "web", Channel: "C1", Mentions: []string{}},
	}
	sv := &stubValidator{}
	reports := RunForEntries(context.Background(), entries, nil, sv)
	if len(reports) != 2 {
		t.Fatalf("reports = %d; want 2", len(reports))
	}
	if sv.calls[0] != "acme/api" || sv.calls[1] != "acme/web" {
		t.Errorf("calls = %v", sv.calls)
	}
}

func TestRunForEntries_WildcardExpansion(t *testing.T) {
	entries := []mappings.Entry{{Org: "beta", Wildcard: true, Channel: "C2", Mentions: []string{}}}
	lister := &stubLister{repos: []string{"r1", "r2", "r3"}}
	sv := &stubValidator{}
	reports := RunForEntries(context.Background(), entries, lister, sv)
	if len(reports) != 3 {
		t.Fatalf("reports = %d; want 3", len(reports))
	}
	want := []string{"beta/r1", "beta/r2", "beta/r3"}
	for i, w := range want {
		if sv.calls[i] != w {
			t.Errorf("call[%d] = %q; want %q", i, sv.calls[i], w)
		}
	}
}

func TestRunForEntries_WildcardWithoutLister_SkipsButReports(t *testing.T) {
	entries := []mappings.Entry{{Org: "beta", Wildcard: true, Channel: "C2", Mentions: []string{}}}
	reports := RunForEntries(context.Background(), entries, nil, &stubValidator{})
	if len(reports) != 1 {
		t.Fatalf("reports = %d; want 1", len(reports))
	}
	r := reports[0]
	if r.Repository != "beta/*" || len(r.Checks) != 1 || r.Checks[0].Status != StatusSkip {
		t.Errorf("expected single skip on beta/*; got %+v", r)
	}
}

func TestRunForEntries_ListerError_BecomesFailingReportAndContinues(t *testing.T) {
	entries := []mappings.Entry{
		{Org: "beta", Wildcard: true, Channel: "C2", Mentions: []string{}},
		{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}},
	}
	lister := &stubLister{err: errors.New("rate-limited")}
	sv := &stubValidator{}
	reports := RunForEntries(context.Background(), entries, lister, sv)
	if len(reports) != 2 {
		t.Fatalf("reports = %d; want 2", len(reports))
	}
	if reports[0].Repository != "beta/*" || reports[0].OK() {
		t.Errorf("first report should be failing beta/*; got %+v", reports[0])
	}
	if reports[1].Repository != "acme/api" || !reports[1].OK() {
		t.Errorf("second report should be OK acme/api; got %+v", reports[1])
	}
}

func TestRunForEntries_PerRepoFailureDoesNotAbort(t *testing.T) {
	entries := []mappings.Entry{
		{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}},
		{Org: "acme", Repo: "web", Channel: "C1", Mentions: []string{}},
	}
	sv := &stubValidator{err: func(r string) bool { return r == "acme/api" }}
	reports := RunForEntries(context.Background(), entries, nil, sv)
	if len(reports) != 2 {
		t.Fatalf("reports = %d; want 2", len(reports))
	}
	if reports[0].OK() || !reports[1].OK() {
		t.Errorf("expected first fail, second ok: %+v", reports)
	}
}
