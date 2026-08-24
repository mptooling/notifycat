package runtime

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

var validatedAt = time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

// TestSplitResults_WarnedEntryIsNeitherCachedNorFatal is the core of the
// advisory contract: a warned entry must not abort startup (absent from failed)
// and must not be cached (absent from successes), so the next boot re-probes it.
func TestSplitResults_WarnedEntryIsNeitherCachedNorFatal(t *testing.T) {
	results := []validationdomain.EntryResult{
		entryResult("acme", "api", validationdomain.StatusOK),
		entryResult("acme", "web", validationdomain.StatusWarn),
		entryResult("acme", "cli", validationdomain.StatusFail),
	}

	successes, warned, failed := splitResults(results, func() time.Time { return validatedAt })

	if len(successes) != 1 {
		t.Fatalf("successes = %v; want only acme/api", successes)
	}
	if _, ok := successes["acme/api"]; !ok {
		t.Errorf("acme/api should be cached; successes = %v", successes)
	}
	if got := strings.Join(warned, ","); got != "acme/web" {
		t.Errorf("warned = %q; want acme/web", got)
	}
	if got := strings.Join(failed, ","); got != "acme/cli" {
		t.Errorf("failed = %q; want acme/cli", got)
	}
}

func TestLogWarnings_LogsEveryWarnedCheck(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	results := []validationdomain.EntryResult{
		entryResult("acme", "api", validationdomain.StatusOK),
		entryResult("acme", "web", validationdomain.StatusWarn),
		entryResult("acme", "cli", validationdomain.StatusFail),
	}

	logWarnings(results, logger)

	out := buf.String()
	if !strings.Contains(out, "startup validate warning") {
		t.Fatalf("no warning logged; got %q", out)
	}
	for _, want := range []string{"entry=acme/web", "check=webhook", "level=WARN"} {
		if !strings.Contains(out, want) {
			t.Errorf("log should contain %q; got %q", want, out)
		}
	}
	if strings.Count(out, "startup validate warning") != 1 {
		t.Errorf("only the warned entry should log; got %q", out)
	}
	if strings.Contains(out, "acme/cli") {
		t.Errorf("a failed check must not be logged as a warning; got %q", out)
	}
}

func entryResult(org, repo string, status validationdomain.Status) validationdomain.EntryResult {
	return validationdomain.EntryResult{
		Entry: routingdomain.Entry{Org: org, Repo: repo, Channel: "C1"},
		Reports: []validationdomain.Report{{
			Repository: org + "/" + repo,
			Checks: []validationdomain.CheckResult{
				{Name: "webhook", Status: status, Detail: "detail for " + repo},
			},
		}},
	}
}
