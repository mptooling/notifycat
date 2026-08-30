package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/validation/domain"
)

func reportWith(statuses ...domain.Status) domain.Report {
	report := domain.Report{Repository: "acme/widgets"}
	for _, status := range statuses {
		report.Checks = append(report.Checks, domain.CheckResult{Name: "check", Status: status})
	}
	return report
}

func TestStatus_String(t *testing.T) {
	wantByStatus := map[domain.Status]string{
		domain.StatusOK:   "OK",
		domain.StatusFail: "FAIL",
		domain.StatusWarn: "WARN",
		domain.StatusSkip: "SKIP",
		domain.Status(99): "UNKNOWN",
	}

	for status, want := range wantByStatus {
		t.Run(want, func(t *testing.T) {
			assert.Equal(t, want, status.String())
		})
	}
}

func TestReport_WarningsDoNotFail(t *testing.T) {
	report := reportWith(domain.StatusOK, domain.StatusWarn)

	assert.True(t, report.OK(), "a warning never fails a report")
	assert.True(t, report.HasWarnings())
}

func TestReport_HasWarnings_FalseWithoutWarnings(t *testing.T) {
	report := reportWith(domain.StatusOK, domain.StatusSkip, domain.StatusFail)

	assert.False(t, report.HasWarnings())
}

// The lock invariant: only an entry that passed with no warnings may be written
// to config.lock, so warned entries re-probe on every boot until the operator
// fixes them.
func TestEntryResult_Cacheable(t *testing.T) {
	testCases := []struct {
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

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := domain.EntryResult{Reports: testCase.reports}

			assert.Equal(t, testCase.wantOK, result.OK())
			assert.Equal(t, testCase.wantWarnings, result.HasWarnings())
			assert.Equal(t, testCase.wantCacheable, result.Cacheable())
		})
	}
}
