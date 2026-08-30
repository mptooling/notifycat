package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/diagnostics/application"
	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

type stubEntrySource struct {
	entries []routingdomain.Entry
}

func (s *stubEntrySource) Entries() []routingdomain.Entry { return s.entries }

type stubChecker struct {
	report func(repository string) validationdomain.Report
	calls  []string
}

func (s *stubChecker) Validate(_ context.Context, repository string) validationdomain.Report {
	s.calls = append(s.calls, repository)
	if s.report != nil {
		return s.report(repository)
	}
	return passingReport(repository)
}

// fakeLockGateway records Plan/Commit/CommitTargeted calls and returns a canned plan.
type fakeLockGateway struct {
	planResult diagnosticsdomain.LockPlan
	planErr    error

	planCalls           int
	commitCalls         int
	commitTargetedCalls []routingdomain.Entry
	lastCommitSuccesses []validationdomain.EntryResult
	lastCommitStale     []string
}

func (f *fakeLockGateway) Plan(entries []routingdomain.Entry, force bool) (diagnosticsdomain.LockPlan, error) {
	f.planCalls++
	if force {
		return diagnosticsdomain.LockPlan{ToValidate: entries}, f.planErr
	}
	return f.planResult, f.planErr
}

func (f *fakeLockGateway) Commit(successes []validationdomain.EntryResult, stale []string) error {
	f.commitCalls++
	f.lastCommitSuccesses = successes
	f.lastCommitStale = stale
	return nil
}

func (f *fakeLockGateway) CommitTargeted(entry routingdomain.Entry) error {
	f.commitTargetedCalls = append(f.commitTargetedCalls, entry)
	return nil
}

func reportWithChecks(repository string, checks ...validationdomain.CheckResult) validationdomain.Report {
	return validationdomain.Report{Repository: repository, Checks: checks}
}

func passingReport(repository string) validationdomain.Report {
	return reportWithChecks(repository, validationdomain.CheckResult{Name: "x", Status: validationdomain.StatusOK, Detail: "ok"})
}

func failingReport(repository string) validationdomain.Report {
	return reportWithChecks(repository, validationdomain.CheckResult{Name: "x", Status: validationdomain.StatusFail, Detail: "boom"})
}

func warningReport(repository string) validationdomain.Report {
	return reportWithChecks(repository,
		validationdomain.CheckResult{Name: "x", Status: validationdomain.StatusOK, Detail: "ok"},
		validationdomain.CheckResult{Name: "webhook", Status: validationdomain.StatusWarn, Detail: "no active webhook on " + repository},
	)
}

func explicitEntries() []routingdomain.Entry {
	return []routingdomain.Entry{
		{Org: "acme", Repo: "api", Channel: "C0123ABCDE", Mentions: []string{"@a"}},
		{Org: "acme", Repo: "web", Channel: "C0123ABCDE", Mentions: []string{"@a"}},
	}
}

func wildcardEntry() routingdomain.Entry {
	return routingdomain.Entry{Org: "beta", Wildcard: true, Channel: "C0456FGHIJ", Mentions: []string{"@b"}}
}

// runValidate drives `notifycat-config validate` and returns its exit code and stdout.
func runValidate(
	t *testing.T,
	entries []routingdomain.Entry,
	checker *stubChecker,
	gateway *fakeLockGateway,
	target string,
	force bool,
) (int, string) {
	t.Helper()

	validator := application.NewMappingsValidator(&stubEntrySource{entries: entries}, checker, nil, gateway)
	var stdout, stderr bytes.Buffer
	code := validator.Validate(context.Background(), target, force, &stdout, &stderr)
	if code != 0 {
		t.Logf("stderr: %s", stderr.String())
	}
	return code, stdout.String()
}

// commitTargetedKeys lists the entries handed to CommitTargeted.
func commitTargetedKeys(gateway *fakeLockGateway) []string {
	keys := make([]string, len(gateway.commitTargetedCalls))
	for i, entry := range gateway.commitTargetedCalls {
		keys[i] = entry.Key()
	}
	return keys
}

// committedSuccessKeys lists the passing entries handed to Commit.
func committedSuccessKeys(gateway *fakeLockGateway) []string {
	var keys []string
	for _, result := range gateway.lastCommitSuccesses {
		if result.OK() {
			keys = append(keys, result.Entry.Key())
		}
	}
	return keys
}

func TestMappingsValidator_Targeted_AllPass_CommitsTargeted(t *testing.T) {
	gateway := &fakeLockGateway{}
	checker := &stubChecker{}

	code, _ := runValidate(t, explicitEntries(), checker, gateway, "acme/api", false)

	assert.Zero(t, code)
	assert.Equal(t, []string{"acme/api"}, checker.calls)
	assert.Equal(t, []string{"acme/api"}, commitTargetedKeys(gateway))
	assert.Zero(t, gateway.commitCalls, "the targeted path never rewrites the whole lock")
}

func TestMappingsValidator_Targeted_Failure_DoesNotCommit(t *testing.T) {
	gateway := &fakeLockGateway{}
	checker := &stubChecker{report: failingReport}

	code, _ := runValidate(t, explicitEntries(), checker, gateway, "acme/api", false)

	assert.Equal(t, 1, code)
	assert.Empty(t, gateway.commitTargetedCalls)
	assert.Zero(t, gateway.commitCalls)
}

// A warning is not a failure (exit 0), but it must not be cached either —
// otherwise the next boot skips the re-probe and the warning silently disappears.
func TestMappingsValidator_Targeted_Warning_DoesNotCommit(t *testing.T) {
	gateway := &fakeLockGateway{}
	checker := &stubChecker{report: warningReport}

	code, stdout := runValidate(t, explicitEntries(), checker, gateway, "acme/api", false)

	assert.Zero(t, code)
	assert.Contains(t, stdout, "WARN")
	assert.Empty(t, gateway.commitTargetedCalls)
}

func TestMappingsValidator_Targeted_WildcardOrg_SkipsLockUpdate(t *testing.T) {
	gateway := &fakeLockGateway{}

	code, _ := runValidate(t, []routingdomain.Entry{wildcardEntry()}, &stubChecker{}, gateway, "beta/anything", false)

	assert.Zero(t, code)
	assert.Empty(t, gateway.commitTargetedCalls, "a wildcard-resolved repo has no lock key of its own")
}

// Warnings never change the exit code; the lock gateway decides on its own what
// may be cached.
func TestMappingsValidator_Full_WarningOnly_ExitsZero(t *testing.T) {
	entries := explicitEntries()
	gateway := &fakeLockGateway{planResult: diagnosticsdomain.LockPlan{ToValidate: entries}}
	checker := &stubChecker{report: warningReport}

	code, stdout := runValidate(t, entries, checker, gateway, "", false)

	assert.Zero(t, code)
	assert.Contains(t, stdout, "WARN")
	assert.Equal(t, 1, gateway.commitCalls)
	for _, result := range gateway.lastCommitSuccesses {
		assert.False(t, result.Cacheable(), "%s warned, so it must not be cached", result.Entry.Key())
	}
}

func TestMappingsValidator_Full_EmptyMappings(t *testing.T) {
	gateway := &fakeLockGateway{}
	checker := &stubChecker{}

	code, stdout := runValidate(t, nil, checker, gateway, "", false)

	assert.Zero(t, code)
	assert.Contains(t, stdout, "no mappings to validate")
	assert.Empty(t, checker.calls)
	assert.Zero(t, gateway.planCalls)
}

func TestMappingsValidator_Full_NoLock_ValidatesAll_Commits(t *testing.T) {
	entries := explicitEntries()
	gateway := &fakeLockGateway{planResult: diagnosticsdomain.LockPlan{ToValidate: entries}}
	checker := &stubChecker{}

	code, _ := runValidate(t, entries, checker, gateway, "", false)

	assert.Zero(t, code)
	assert.Equal(t, []string{"acme/api", "acme/web"}, checker.calls)
	assert.Equal(t, 1, gateway.commitCalls)
	assert.Len(t, gateway.lastCommitSuccesses, 2)
}

func TestMappingsValidator_Full_UpToDateLock_SkipsValidation(t *testing.T) {
	gateway := &fakeLockGateway{planResult: diagnosticsdomain.LockPlan{}}
	checker := &stubChecker{}

	code, stdout := runValidate(t, explicitEntries(), checker, gateway, "", false)

	assert.Zero(t, code)
	assert.Empty(t, checker.calls)
	assert.Contains(t, stdout, "lock is up to date")
	assert.Zero(t, gateway.commitCalls, "nothing to validate means nothing to write")
}

func TestMappingsValidator_Full_Force_ValidatesAll(t *testing.T) {
	entries := explicitEntries()
	// Without force this plan would validate nothing.
	gateway := &fakeLockGateway{planResult: diagnosticsdomain.LockPlan{}}
	checker := &stubChecker{}

	code, _ := runValidate(t, entries, checker, gateway, "", true)

	assert.Zero(t, code)
	assert.Equal(t, []string{"acme/api", "acme/web"}, checker.calls)
	assert.Equal(t, 1, gateway.commitCalls)
}

func TestMappingsValidator_Full_PartialFailure_OnlySuccessesInCommit(t *testing.T) {
	entries := explicitEntries()
	gateway := &fakeLockGateway{planResult: diagnosticsdomain.LockPlan{ToValidate: entries}}
	checker := &stubChecker{report: func(repository string) validationdomain.Report {
		if repository == "acme/api" {
			return failingReport(repository)
		}
		return passingReport(repository)
	}}

	code, _ := runValidate(t, entries, checker, gateway, "", false)

	assert.Equal(t, 1, code)
	assert.Equal(t, 1, gateway.commitCalls, "the passing entries are still committed")
	assert.Equal(t, []string{"acme/web"}, committedSuccessKeys(gateway))
}

func TestList_ShowsEveryChannel(t *testing.T) {
	entries := &stubEntrySource{entries: []routingdomain.Entry{
		{Org: "zeta", Repo: "api", Channel: "C0ZETA0001",
			ExtraChannels: []string{"C0ZETA0002", "C0ZETA0003"}},
		{Org: "acme", Repo: "api", Channel: "C0AAA0001", Mentions: []string{"<@U0A>"}},
	}}
	var stdout bytes.Buffer

	code := application.List(entries, &stdout)

	require.Zero(t, code)
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	require.Len(t, lines, 3, "header plus one row per entry:\n%s", stdout.String())
	assert.Contains(t, lines[1], "C0ZETA0001")
	assert.Contains(t, lines[1], "C0ZETA0002")
	assert.Contains(t, lines[1], "C0ZETA0003")
	assert.Contains(t, lines[2], "C0AAA0001")
	assert.NotContains(t, lines[2], ",", "a single-channel entry lists one channel")
}
