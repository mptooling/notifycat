package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	fails func(repository string) bool
}

func (s *stubValidator) Validate(_ context.Context, repository string) domain.Report {
	s.calls = append(s.calls, repository)
	status := domain.StatusOK
	detail := "ok"
	if s.fails != nil && s.fails(repository) {
		status, detail = domain.StatusFail, "boom"
	}
	return domain.Report{Repository: repository, Checks: []domain.CheckResult{{Name: "x", Status: status, Detail: detail}}}
}

func explicitEntries(repos ...string) []routingdomain.Entry {
	entries := make([]routingdomain.Entry, len(repos))
	for i, repo := range repos {
		entries[i] = routingdomain.Entry{Org: "acme", Repo: repo, Channel: "C1", Mentions: []string{}}
	}
	return entries
}

func wildcardEntry(org string) routingdomain.Entry {
	return routingdomain.Entry{Org: org, Wildcard: true, Channel: "C2", Mentions: []string{}}
}

func TestRunForEntries_ExplicitOnly(t *testing.T) {
	validator := &stubValidator{}

	results := application.RunForEntries(context.Background(), explicitEntries("api", "web"), nil, validator)

	require.Len(t, results, 2)
	assert.Equal(t, []string{"acme/api", "acme/web"}, validator.calls)
	assert.Len(t, results[0].Reports, 1)
	assert.Len(t, results[1].Reports, 1)
	assert.True(t, results[0].OK())
	assert.True(t, results[1].OK())
}

func TestRunForEntries_WildcardExpansion(t *testing.T) {
	lister := &stubLister{repos: []string{"r1", "r2", "r3"}}
	validator := &stubValidator{}

	results := application.RunForEntries(context.Background(), []routingdomain.Entry{wildcardEntry("beta")}, lister, validator)

	require.Len(t, results, 1)
	assert.Len(t, results[0].Reports, 3, "one report per expanded repo")
	assert.Equal(t, []string{"beta/r1", "beta/r2", "beta/r3"}, validator.calls)
	assert.True(t, results[0].OK())
}

func TestRunForEntries_WildcardWithoutLister_SkipsButReports(t *testing.T) {
	results := application.RunForEntries(context.Background(), []routingdomain.Entry{wildcardEntry("beta")}, nil, &stubValidator{})

	require.Len(t, results, 1)
	require.Len(t, results[0].Reports, 1)
	assert.Equal(t, "beta/*", results[0].Reports[0].Repository)
	assert.Equal(t, domain.StatusSkip, results[0].Reports[0].Checks[0].Status)
	assert.True(t, results[0].OK(), "a skip is not a failure")
}

// Failing to list an org's repositories is external state (token scope, rate
// limit), so it warns rather than failing the entry — but the entry stays out of
// the lock via Cacheable.
func TestRunForEntries_ListerError_WarnsAndContinues(t *testing.T) {
	entries := append([]routingdomain.Entry{wildcardEntry("beta")}, explicitEntries("api")...)
	lister := &stubLister{err: errors.New("rate-limited")}
	validator := &stubValidator{}

	results := application.RunForEntries(context.Background(), entries, lister, validator)

	require.Len(t, results, 2)
	assert.True(t, results[0].OK())
	assert.True(t, results[0].HasWarnings())
	assert.False(t, results[0].Cacheable(), "a warned entry is re-probed on the next boot")
	assert.Equal(t, "org-repos", results[0].Reports[0].Checks[0].Name)
	assert.Equal(t, domain.StatusWarn, results[0].Reports[0].Checks[0].Status)
	assert.True(t, results[1].OK(), "the next entry is still validated")
	assert.Equal(t, "acme/api", results[1].Reports[0].Repository)
}

func TestRunForEntries_PerRepoFailureDoesNotAbort(t *testing.T) {
	validator := &stubValidator{fails: func(repository string) bool { return repository == "acme/api" }}

	results := application.RunForEntries(context.Background(), explicitEntries("api", "web"), nil, validator)

	require.Len(t, results, 2)
	assert.False(t, results[0].OK())
	assert.True(t, results[1].OK())
}
