package infrastructure

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domain "github.com/mptooling/notifycat/internal/routing/domain"
)

func TestLock_WriteThenRead_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mappings.lock")
	validatedAt := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	want := Lock{
		Version: 1,
		Entries: map[string]LockEntry{
			"acme/api": {SHA256: "abc", ValidatedAt: validatedAt},
			"beta/*":   {SHA256: "def", ValidatedAt: validatedAt},
		},
	}
	require.NoError(t, WriteLock(path, want))

	got, err := ReadLock(path)

	require.NoError(t, err)
	assert.Equal(t, want.Entries, got.Entries)
}

func TestLock_Read_Missing(t *testing.T) {
	got, err := ReadLock(filepath.Join(t.TempDir(), "no.lock"))

	require.NoError(t, err, "a missing lock is a cold start, not a failure")
	assert.Empty(t, got.Entries)
}

func TestLock_Read_Malformed_ReturnsEmptyAndError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.lock")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	got, err := ReadLock(path)

	require.Error(t, err, "the caller warns on a corrupt lock")
	assert.Empty(t, got.Entries)
}

func TestDiffEntries(t *testing.T) {
	current := []domain.Entry{
		{Org: "acme", Repo: "api", Channel: "C1", Mentions: []string{}},
		{Org: "acme", Repo: "web", Channel: "C1", Mentions: []string{}},
		{Org: "beta", Wildcard: true, Channel: "C2", Mentions: []string{}},
	}
	lock := Lock{
		Version: 1,
		Entries: map[string]LockEntry{
			"acme/api": {SHA256: current[0].Hash()},
			"beta/*":   {SHA256: "stale-different-hash"},
			"old/dead": {SHA256: "x"},
		},
	}

	diff := DiffEntries(current, lock)

	needsKeys := make([]string, 0, len(diff.Needs))
	for _, entry := range diff.Needs {
		needsKeys = append(needsKeys, entry.Key())
	}
	assert.ElementsMatch(t, []string{"acme/web", "beta/*"}, needsKeys, "new and changed entries revalidate, unchanged ones do not")
	assert.Equal(t, []string{"old/dead"}, diff.Stale)
}

func TestMergeLock(t *testing.T) {
	previous := Lock{
		Version: 1,
		Entries: map[string]LockEntry{
			"acme/api": {SHA256: "keep"},
			"old/dead": {SHA256: "x"},
		},
	}
	validated := map[string]LockEntry{
		"acme/web": {SHA256: "new"},
	}

	got := MergeLock(previous, validated, []string{"old/dead"})

	assert.NotContains(t, got.Entries, "old/dead")
	assert.Equal(t, "keep", got.Entries["acme/api"].SHA256)
	assert.Equal(t, "new", got.Entries["acme/web"].SHA256)
}
