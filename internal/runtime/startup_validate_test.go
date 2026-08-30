package runtime //nolint:revive // internal test for the composition root's unexported splitResults/logWarnings; the package name mirrors module.go

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

var validatedAt = time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)

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

// mixedResults holds one passing, one warned, and one failed entry.
func mixedResults() []validationdomain.EntryResult {
	return []validationdomain.EntryResult{
		entryResult("acme", "api", validationdomain.StatusOK),
		entryResult("acme", "web", validationdomain.StatusWarn),
		entryResult("acme", "cli", validationdomain.StatusFail),
	}
}

// The core of the advisory contract: a warned entry must not abort startup
// (absent from failed) and must not be cached (absent from successes), so the
// next boot re-probes it.
func TestSplitResults_WarnedEntryIsNeitherCachedNorFatal(t *testing.T) {
	successes, warned, failed := splitResults(mixedResults(), func() time.Time { return validatedAt })

	assert.Len(t, successes, 1)
	assert.Contains(t, successes, "acme/api")
	assert.Equal(t, []string{"acme/web"}, warned)
	assert.Equal(t, []string{"acme/cli"}, failed)
}

func TestLogWarnings_LogsEveryWarnedCheck(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn}))

	logWarnings(mixedResults(), logger)

	assert.Contains(t, logged.String(), "entry=acme/web")
	assert.Contains(t, logged.String(), "check=webhook")
	assert.Contains(t, logged.String(), "level=WARN")
	assert.Equal(t, 1, strings.Count(logged.String(), "startup validate warning"), "only the warned entry logs")
	assert.NotContains(t, logged.String(), "acme/cli", "a failed check is not a warning")
}
