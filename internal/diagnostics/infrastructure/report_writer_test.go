package infrastructure_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	"github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// writeReport renders sections and returns the overall verdict plus the output.
func writeReport(sections []diagnosticsdomain.Section) (bool, string) {
	var rendered bytes.Buffer
	ok := infrastructure.WriteReport(&rendered, sections)
	return ok, rendered.String()
}

func TestWriteReport_AllOK_ReturnsTrue(t *testing.T) {
	sections := []diagnosticsdomain.Section{
		{Name: "config", Checks: []validationdomain.CheckResult{
			{Name: "GITHUB_WEBHOOK_SECRET", Status: validationdomain.StatusOK, Detail: "set"},
			{Name: "SLACK_BOT_TOKEN", Status: validationdomain.StatusOK, Detail: "set"},
		}},
	}

	ok, out := writeReport(sections)

	assert.True(t, ok)
	assert.Contains(t, out, "config")
	assert.Contains(t, out, "OK")
}

func TestWriteReport_AnyFail_ReturnsFalse(t *testing.T) {
	sections := []diagnosticsdomain.Section{
		{Name: "config", Checks: []validationdomain.CheckResult{
			{Name: "GITHUB_WEBHOOK_SECRET", Status: validationdomain.StatusOK, Detail: "set"},
		}},
		{Name: "database", Checks: []validationdomain.CheckResult{
			{Name: "open", Status: validationdomain.StatusFail, Detail: "no such file: /missing/path.db"},
		}},
	}

	ok, out := writeReport(sections)

	assert.False(t, ok)
	assert.Contains(t, out, "FAIL")
	assert.Contains(t, out, "no such file: /missing/path.db", "the detail carries the remediation hint")
}

func TestWriteReport_SkipDoesNotFailOverall(t *testing.T) {
	sections := []diagnosticsdomain.Section{
		{Name: "github", Checks: []validationdomain.CheckResult{
			{Name: "webhook-events", Status: validationdomain.StatusSkip, Detail: "GITHUB_TOKEN not set"},
		}},
	}

	ok, out := writeReport(sections)

	assert.True(t, ok)
	assert.Contains(t, out, "SKIP")
}

// The doctor's exit code: an advisory webhook problem prints WARN with its hint
// and still exits 0.
func TestWriteReport_WarnDoesNotFailOverall(t *testing.T) {
	sections := []diagnosticsdomain.Section{
		{Name: "acme/widgets", Checks: []validationdomain.CheckResult{
			{Name: "webhook", Status: validationdomain.StatusWarn, Detail: "no active webhook on acme/widgets points at notifycat"},
		}},
	}

	ok, out := writeReport(sections)

	assert.True(t, ok)
	assert.Contains(t, out, "WARN")
	assert.Contains(t, out, "no active webhook on acme/widgets points at notifycat")
}

func TestWriteReport_EmptySections(t *testing.T) {
	ok, _ := writeReport(nil)

	assert.True(t, ok, "nothing to check is not a failure")
}
