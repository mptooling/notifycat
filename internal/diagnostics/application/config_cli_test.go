package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/diagnostics/application"
	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// ---- fakes -------------------------------------------------------------------

type stubEntrySource struct {
	entries []routingdomain.Entry
}

func (s *stubEntrySource) Entries() []routingdomain.Entry { return s.entries }

type stubChecker struct {
	fn    func(ctx context.Context, repository string) validationdomain.Report
	calls []string
}

func (s *stubChecker) Validate(ctx context.Context, repository string) validationdomain.Report {
	s.calls = append(s.calls, repository)
	if s.fn != nil {
		return s.fn(ctx, repository)
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

// ---- helpers -----------------------------------------------------------------

func passingReport(repository string) validationdomain.Report {
	return validationdomain.Report{
		Repository: repository,
		Checks:     []validationdomain.CheckResult{{Name: "x", Status: validationdomain.StatusOK, Detail: "ok"}},
	}
}

func failingReport(repository string) validationdomain.Report {
	return validationdomain.Report{
		Repository: repository,
		Checks:     []validationdomain.CheckResult{{Name: "x", Status: validationdomain.StatusFail, Detail: "boom"}},
	}
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

// ---- tests -------------------------------------------------------------------

func TestMappingsValidator_Targeted_AllPass_CommitsTargeted(t *testing.T) {
	gateway := &fakeLockGateway{}
	checker := &stubChecker{}
	entries := explicitEntries()
	source := &stubEntrySource{entries: entries}
	validator := application.NewMappingsValidator(source, checker, nil, gateway)
	var out, errOut bytes.Buffer

	code := validator.Validate(context.Background(), "acme/api", false, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d; stderr=%s", code, errOut.String())
	}
	if len(checker.calls) != 1 || checker.calls[0] != "acme/api" {
		t.Errorf("checker calls = %v", checker.calls)
	}
	if len(gateway.commitTargetedCalls) != 1 || gateway.commitTargetedCalls[0].Key() != "acme/api" {
		t.Errorf("CommitTargeted calls = %v", gateway.commitTargetedCalls)
	}
	if gateway.commitCalls != 0 {
		t.Errorf("Commit should not be called in targeted path; got %d calls", gateway.commitCalls)
	}
}

func TestMappingsValidator_Targeted_Failure_DoesNotCommit(t *testing.T) {
	gateway := &fakeLockGateway{}
	checker := &stubChecker{fn: func(_ context.Context, r string) validationdomain.Report { return failingReport(r) }}
	source := &stubEntrySource{entries: explicitEntries()}
	validator := application.NewMappingsValidator(source, checker, nil, gateway)
	var out, errOut bytes.Buffer

	code := validator.Validate(context.Background(), "acme/api", false, &out, &errOut)

	if code != 1 {
		t.Fatalf("exit = %d; want 1", code)
	}
	if len(gateway.commitTargetedCalls) != 0 {
		t.Errorf("CommitTargeted must not be called on failure; got %v", gateway.commitTargetedCalls)
	}
	if gateway.commitCalls != 0 {
		t.Errorf("Commit must not be called on failure; got %d calls", gateway.commitCalls)
	}
}

func TestMappingsValidator_Targeted_WildcardOrg_SkipsLockUpdate(t *testing.T) {
	gateway := &fakeLockGateway{}
	checker := &stubChecker{}
	source := &stubEntrySource{entries: []routingdomain.Entry{wildcardEntry()}}
	validator := application.NewMappingsValidator(source, checker, nil, gateway)
	var out, errOut bytes.Buffer

	code := validator.Validate(context.Background(), "beta/anything", false, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d; stderr=%s", code, errOut.String())
	}
	if len(gateway.commitTargetedCalls) != 0 {
		t.Errorf("wildcard-resolved targeted run must not call CommitTargeted; got %v", gateway.commitTargetedCalls)
	}
}

func TestMappingsValidator_Full_EmptyMappings(t *testing.T) {
	gateway := &fakeLockGateway{}
	checker := &stubChecker{}
	source := &stubEntrySource{entries: nil}
	validator := application.NewMappingsValidator(source, checker, nil, gateway)
	var out, errOut bytes.Buffer

	code := validator.Validate(context.Background(), "", false, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "no mappings to validate") {
		t.Errorf("expected friendly empty-state message; got %q", out.String())
	}
	if len(checker.calls) != 0 {
		t.Errorf("checker should not have been called: %v", checker.calls)
	}
	if gateway.planCalls != 0 {
		t.Errorf("Plan should not be called for empty mappings; got %d calls", gateway.planCalls)
	}
}

func TestMappingsValidator_Full_NoLock_ValidatesAll_Commits(t *testing.T) {
	entries := explicitEntries()
	// Simulate "no lock" by returning all entries in ToValidate.
	gateway := &fakeLockGateway{
		planResult: diagnosticsdomain.LockPlan{ToValidate: entries},
	}
	checker := &stubChecker{}
	source := &stubEntrySource{entries: entries}
	validator := application.NewMappingsValidator(source, checker, nil, gateway)
	var out, errOut bytes.Buffer

	code := validator.Validate(context.Background(), "", false, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d; stderr=%s", code, errOut.String())
	}
	if len(checker.calls) != 2 {
		t.Errorf("expected 2 calls (api, web); got %v", checker.calls)
	}
	if gateway.commitCalls != 1 {
		t.Errorf("expected 1 Commit call; got %d", gateway.commitCalls)
	}
	// Both entries passed so both successes should be in the commit.
	if len(gateway.lastCommitSuccesses) != 2 {
		t.Errorf("expected 2 successes in Commit; got %d", len(gateway.lastCommitSuccesses))
	}
}

func TestMappingsValidator_Full_UpToDateLock_SkipsValidation(t *testing.T) {
	entries := explicitEntries()
	// Simulate "up to date" by returning an empty ToValidate.
	gateway := &fakeLockGateway{
		planResult: diagnosticsdomain.LockPlan{ToValidate: nil, Stale: nil},
	}
	checker := &stubChecker{}
	source := &stubEntrySource{entries: entries}
	validator := application.NewMappingsValidator(source, checker, nil, gateway)
	var out, errOut bytes.Buffer

	code := validator.Validate(context.Background(), "", false, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(checker.calls) != 0 {
		t.Errorf("expected zero calls; got %v", checker.calls)
	}
	if !strings.Contains(out.String(), "lock is up to date") {
		t.Errorf("expected up-to-date hint; got %q", out.String())
	}
	if gateway.commitCalls != 0 {
		t.Errorf("Commit should not be called when nothing to validate; got %d", gateway.commitCalls)
	}
}

func TestMappingsValidator_Full_Force_ValidatesAll(t *testing.T) {
	entries := explicitEntries()
	// Force: Plan returns all entries regardless of the planResult field.
	gateway := &fakeLockGateway{
		planResult: diagnosticsdomain.LockPlan{ToValidate: nil}, // would be empty without force
	}
	checker := &stubChecker{}
	source := &stubEntrySource{entries: entries}
	validator := application.NewMappingsValidator(source, checker, nil, gateway)
	var out, errOut bytes.Buffer

	code := validator.Validate(context.Background(), "", true, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit = %d; stderr=%s", code, errOut.String())
	}
	if len(checker.calls) != 2 {
		t.Errorf("force should revalidate everything; got calls=%v", checker.calls)
	}
	if gateway.commitCalls != 1 {
		t.Errorf("expected 1 Commit call after force run; got %d", gateway.commitCalls)
	}
}

func TestMappingsValidator_Full_PartialFailure_OnlySuccessesInCommit(t *testing.T) {
	entries := explicitEntries()
	gateway := &fakeLockGateway{
		planResult: diagnosticsdomain.LockPlan{ToValidate: entries},
	}
	checker := &stubChecker{fn: func(_ context.Context, r string) validationdomain.Report {
		if r == "acme/api" {
			return failingReport(r)
		}
		return passingReport(r)
	}}
	source := &stubEntrySource{entries: entries}
	validator := application.NewMappingsValidator(source, checker, nil, gateway)
	var out, errOut bytes.Buffer

	code := validator.Validate(context.Background(), "", false, &out, &errOut)

	if code != 1 {
		t.Fatalf("exit = %d; want 1 (one entry failed)", code)
	}
	if gateway.commitCalls != 1 {
		t.Errorf("expected 1 Commit call even on partial failure; got %d", gateway.commitCalls)
	}
	// Only the passing entry (acme/web) should be in the successes.
	successKeys := map[string]bool{}
	for _, result := range gateway.lastCommitSuccesses {
		if result.OK() {
			successKeys[result.Entry.Key()] = true
		}
	}
	if successKeys["acme/api"] {
		t.Errorf("acme/api failed; should not appear as success in Commit")
	}
	if !successKeys["acme/web"] {
		t.Errorf("acme/web passed; should appear as success in Commit")
	}
}
