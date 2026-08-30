package infrastructure

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	routinginfra "github.com/mptooling/notifycat/internal/routing/infrastructure"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

func fixedClock() func() time.Time {
	now := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	return func() time.Time { return now }
}

func tempLockPath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "mappings.lock")
}

func explicitEntries() []routingdomain.Entry {
	return []routingdomain.Entry{
		{Org: "acme", Repo: "api", Channel: "C0123ABCDE"},
		{Org: "acme", Repo: "web", Channel: "C0123ABCDE"},
	}
}

// seedLock writes a lock that already holds the current hash of every entry.
func seedLock(t *testing.T, lockPath string, clock func() time.Time, entries []routingdomain.Entry) {
	t.Helper()

	prior := routinginfra.Lock{Version: routinginfra.LockVersion, Entries: map[string]routinginfra.LockEntry{}}
	for _, entry := range entries {
		prior.Entries[entry.Key()] = routinginfra.LockEntry{SHA256: entry.Hash(), ValidatedAt: clock()}
	}
	require.NoError(t, routinginfra.WriteLock(lockPath, prior))
}

func readLock(t *testing.T, lockPath string) routinginfra.Lock {
	t.Helper()

	lock, err := routinginfra.ReadLock(lockPath)
	require.NoError(t, err)
	return lock
}

func resultWith(entry routingdomain.Entry, status validationdomain.Status, detail string) validationdomain.EntryResult {
	return validationdomain.EntryResult{
		Entry: entry,
		Reports: []validationdomain.Report{
			{Repository: entry.Key(), Checks: []validationdomain.CheckResult{{Name: "x", Status: status, Detail: detail}}},
		},
	}
}

func passingResult(entry routingdomain.Entry) validationdomain.EntryResult {
	return resultWith(entry, validationdomain.StatusOK, "ok")
}

func failingResult(entry routingdomain.Entry) validationdomain.EntryResult {
	return resultWith(entry, validationdomain.StatusFail, "boom")
}

func warningResult(entry routingdomain.Entry) validationdomain.EntryResult {
	return resultWith(entry, validationdomain.StatusWarn, "no active webhook")
}

func TestLockGateway_Plan_NoLock_ReturnsAllEntries(t *testing.T) {
	entries := explicitEntries()
	gateway := NewLockGateway(tempLockPath(t), fixedClock())

	plan, err := gateway.Plan(entries, false)

	require.NoError(t, err)
	assert.Len(t, plan.ToValidate, len(entries), "a missing lock is a cold start")
	assert.Empty(t, plan.Stale)
}

func TestLockGateway_Plan_UpToDate_ReturnsEmpty(t *testing.T) {
	lockPath := tempLockPath(t)
	clock := fixedClock()
	entries := explicitEntries()
	seedLock(t, lockPath, clock, entries)
	gateway := NewLockGateway(lockPath, clock)

	plan, err := gateway.Plan(entries, false)

	require.NoError(t, err)
	assert.Empty(t, plan.ToValidate)
}

func TestLockGateway_Plan_Force_ReturnsAllEntries(t *testing.T) {
	lockPath := tempLockPath(t)
	clock := fixedClock()
	entries := explicitEntries()
	seedLock(t, lockPath, clock, entries)
	gateway := NewLockGateway(lockPath, clock)

	plan, err := gateway.Plan(entries, true)

	require.NoError(t, err)
	assert.Len(t, plan.ToValidate, len(entries), "force ignores the lock")
}

func TestLockGateway_Commit_WritesAllSuccessesToLock(t *testing.T) {
	lockPath := tempLockPath(t)
	gateway := NewLockGateway(lockPath, fixedClock())
	entries := explicitEntries()

	err := gateway.Commit([]validationdomain.EntryResult{passingResult(entries[0]), passingResult(entries[1])}, nil)

	require.NoError(t, err)
	lock := readLock(t, lockPath)
	for _, entry := range entries {
		require.Contains(t, lock.Entries, entry.Key())
		assert.Equal(t, entry.Hash(), lock.Entries[entry.Key()].SHA256)
	}
}

func TestLockGateway_Commit_PartialFailure_OnlySuccessesEnterLock(t *testing.T) {
	lockPath := tempLockPath(t)
	gateway := NewLockGateway(lockPath, fixedClock())
	entries := explicitEntries()

	err := gateway.Commit([]validationdomain.EntryResult{failingResult(entries[0]), passingResult(entries[1])}, nil)

	require.NoError(t, err)
	lock := readLock(t, lockPath)
	assert.NotContains(t, lock.Entries, "acme/api", "a failed entry is never cached")
	assert.Contains(t, lock.Entries, "acme/web")
}

// Keeps the CLI and the server in agreement: a warned entry is never cached, so
// both re-probe it and re-surface the warning until the operator fixes it.
func TestLockGateway_Commit_WarnedEntryStaysOutOfLock(t *testing.T) {
	lockPath := tempLockPath(t)
	gateway := NewLockGateway(lockPath, fixedClock())
	entries := explicitEntries()

	err := gateway.Commit([]validationdomain.EntryResult{warningResult(entries[0]), passingResult(entries[1])}, nil)

	require.NoError(t, err)
	lock := readLock(t, lockPath)
	assert.NotContains(t, lock.Entries, "acme/api")
	assert.Contains(t, lock.Entries, "acme/web")
}

func TestLockGateway_Commit_StaleKeysDropped(t *testing.T) {
	lockPath := tempLockPath(t)
	clock := fixedClock()
	entries := explicitEntries()
	seedLock(t, lockPath, clock, entries)
	stale := readLock(t, lockPath)
	stale.Entries["acme/old"] = routinginfra.LockEntry{SHA256: "deadbeef", ValidatedAt: clock()}
	require.NoError(t, routinginfra.WriteLock(lockPath, stale))
	gateway := NewLockGateway(lockPath, clock)

	err := gateway.Commit(nil, []string{"acme/old"})

	require.NoError(t, err)
	lock := readLock(t, lockPath)
	assert.NotContains(t, lock.Entries, "acme/old")
	assert.Contains(t, lock.Entries, "acme/api", "the surviving entries are preserved")
}

func TestLockGateway_CommitTargeted_WritesEntry(t *testing.T) {
	lockPath := tempLockPath(t)
	gateway := NewLockGateway(lockPath, fixedClock())
	entry := explicitEntries()[0]

	err := gateway.CommitTargeted(entry)

	require.NoError(t, err)
	lock := readLock(t, lockPath)
	require.Contains(t, lock.Entries, "acme/api")
	assert.Equal(t, entry.Hash(), lock.Entries["acme/api"].SHA256)
}

func TestLockGateway_CommitTargeted_NoFileBeforeCall(t *testing.T) {
	lockPath := tempLockPath(t)
	gateway := NewLockGateway(lockPath, fixedClock())
	require.NoFileExists(t, lockPath)

	err := gateway.CommitTargeted(explicitEntries()[0])

	require.NoError(t, err)
	assert.FileExists(t, lockPath, "CommitTargeted creates the lock it needs")
}
