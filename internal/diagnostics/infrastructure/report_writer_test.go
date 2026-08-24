package infrastructure_test

import (
	"bytes"
	"strings"
	"testing"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	"github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

func TestWriteReport_AllOK_ReturnsTrue(t *testing.T) {
	sections := []diagnosticsdomain.Section{
		{Name: "config", Checks: []validationdomain.CheckResult{
			{Name: "GITHUB_WEBHOOK_SECRET", Status: validationdomain.StatusOK, Detail: "set"},
			{Name: "SLACK_BOT_TOKEN", Status: validationdomain.StatusOK, Detail: "set"},
		}},
	}
	var buf bytes.Buffer
	ok := infrastructure.WriteReport(&buf, sections)
	if !ok {
		t.Fatalf("WriteReport returned false for all-OK sections")
	}
	out := buf.String()
	if !strings.Contains(out, "config") {
		t.Errorf("output missing section name: %q", out)
	}
	if !strings.Contains(out, "OK") {
		t.Errorf("output missing OK status: %q", out)
	}
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
	var buf bytes.Buffer
	ok := infrastructure.WriteReport(&buf, sections)
	if ok {
		t.Fatalf("WriteReport returned true despite a FAIL check")
	}
	out := buf.String()
	if !strings.Contains(out, "FAIL") {
		t.Errorf("output missing FAIL status: %q", out)
	}
	if !strings.Contains(out, "no such file: /missing/path.db") {
		t.Errorf("output missing FAIL detail (remediation hint): %q", out)
	}
}

func TestWriteReport_SkipDoesNotFailOverall(t *testing.T) {
	sections := []diagnosticsdomain.Section{
		{Name: "github", Checks: []validationdomain.CheckResult{
			{Name: "webhook-events", Status: validationdomain.StatusSkip, Detail: "GITHUB_TOKEN not set"},
		}},
	}
	var buf bytes.Buffer
	ok := infrastructure.WriteReport(&buf, sections)
	if !ok {
		t.Fatalf("WriteReport returned false for sections containing only SKIPs")
	}
	if !strings.Contains(buf.String(), "SKIP") {
		t.Errorf("output missing SKIP status: %q", buf.String())
	}
}

// TestWriteReport_WarnDoesNotFailOverall pins the doctor's exit code: an
// advisory webhook problem prints WARN with its hint and still exits 0.
func TestWriteReport_WarnDoesNotFailOverall(t *testing.T) {
	sections := []diagnosticsdomain.Section{
		{Name: "acme/widgets", Checks: []validationdomain.CheckResult{
			{Name: "webhook", Status: validationdomain.StatusWarn, Detail: "no active webhook on acme/widgets points at notifycat"},
		}},
	}
	var buf bytes.Buffer
	ok := infrastructure.WriteReport(&buf, sections)
	if !ok {
		t.Fatalf("WriteReport returned false for a WARN-only section")
	}
	out := buf.String()
	if !strings.Contains(out, "WARN") {
		t.Errorf("output missing WARN status: %q", out)
	}
	if !strings.Contains(out, "no active webhook on acme/widgets points at notifycat") {
		t.Errorf("output missing WARN detail: %q", out)
	}
}

func TestWriteReport_EmptySections(t *testing.T) {
	var buf bytes.Buffer
	if !infrastructure.WriteReport(&buf, nil) {
		t.Errorf("WriteReport(nil) returned false; want true (no failures)")
	}
}
