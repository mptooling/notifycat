package domain_test

import (
	"testing"

	"github.com/mptooling/notifycat/internal/validation/domain"
)

func TestStatus_String(t *testing.T) {
	cases := map[domain.Status]string{
		domain.StatusOK:   "OK",
		domain.StatusFail: "FAIL",
		domain.StatusWarn: "WARN",
		domain.StatusSkip: "SKIP",
		domain.Status(99): "UNKNOWN",
	}
	for status, want := range cases {
		if got := status.String(); got != want {
			t.Errorf("Status(%d).String() = %q; want %q", status, got, want)
		}
	}
}

func TestReport_WarningsDoNotFail(t *testing.T) {
	report := reportWith(domain.StatusOK, domain.StatusWarn)

	if !report.OK() {
		t.Error("a warned report must still be OK; warnings never fail a report")
	}
	if !report.HasWarnings() {
		t.Error("HasWarnings() should be true when a check warned")
	}
}

func TestReport_HasWarnings_FalseWithoutWarnings(t *testing.T) {
	report := reportWith(domain.StatusOK, domain.StatusSkip, domain.StatusFail)

	if report.HasWarnings() {
		t.Error("HasWarnings() should be false when no check warned")
	}
}

// TestEntryResult_Cacheable pins the lock invariant: only an entry that passed
// with no warnings may be written to config.lock, so warned entries re-probe on
// every boot until the operator fixes them.
func TestEntryResult_Cacheable(t *testing.T) {
	cases := []struct {
		name          string
		reports       []domain.Report
		wantOK        bool
		wantWarnings  bool
		wantCacheable bool
	}{
		{
			name:          "all checks passed",
			reports:       []domain.Report{reportWith(domain.StatusOK, domain.StatusSkip)},
			wantOK:        true,
			wantCacheable: true,
		},
		{
			name:          "one report warned",
			reports:       []domain.Report{reportWith(domain.StatusOK), reportWith(domain.StatusWarn)},
			wantOK:        true,
			wantWarnings:  true,
			wantCacheable: false,
		},
		{
			name:          "one report failed",
			reports:       []domain.Report{reportWith(domain.StatusFail)},
			wantOK:        false,
			wantCacheable: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := domain.EntryResult{Reports: tc.reports}
			if got := result.OK(); got != tc.wantOK {
				t.Errorf("OK() = %v; want %v", got, tc.wantOK)
			}
			if got := result.HasWarnings(); got != tc.wantWarnings {
				t.Errorf("HasWarnings() = %v; want %v", got, tc.wantWarnings)
			}
			if got := result.Cacheable(); got != tc.wantCacheable {
				t.Errorf("Cacheable() = %v; want %v", got, tc.wantCacheable)
			}
		})
	}
}

func reportWith(statuses ...domain.Status) domain.Report {
	report := domain.Report{Repository: "acme/widgets"}
	for _, status := range statuses {
		report.Checks = append(report.Checks, domain.CheckResult{Name: "check", Status: status})
	}
	return report
}
