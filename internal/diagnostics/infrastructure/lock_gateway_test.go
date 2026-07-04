package infrastructure

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
	dir := t.TempDir()
	return filepath.Join(dir, "mappings.lock")
}

func explicitEntries() []routingdomain.Entry {
	return []routingdomain.Entry{
		{Org: "acme", Repo: "api", Channel: "C0123ABCDE"},
		{Org: "acme", Repo: "web", Channel: "C0123ABCDE"},
	}
}

func passingResult(entry routingdomain.Entry) validationdomain.EntryResult {
	return validationdomain.EntryResult{
		Entry: entry,
		Reports: []validationdomain.Report{
			{Repository: entry.Key(), Checks: []validationdomain.CheckResult{{Name: "x", Status: validationdomain.StatusOK, Detail: "ok"}}},
		},
	}
}

func failingResult(entry routingdomain.Entry) validationdomain.EntryResult {
	return validationdomain.EntryResult{
		Entry: entry,
		Reports: []validationdomain.Report{
			{Repository: entry.Key(), Checks: []validationdomain.CheckResult{{Name: "x", Status: validationdomain.StatusFail, Detail: "boom"}}},
		},
	}
}

// TestLockGateway_Plan_NoLock_ReturnsAllEntries verifies that Plan with a
// missing lock file returns every entry for validation.
func TestLockGateway_Plan_NoLock_ReturnsAllEntries(t *testing.T) {
	lockPath := tempLockPath(t)
	gateway := NewLockGateway(lockPath, fixedClock())
	entries := explicitEntries()

	plan, err := gateway.Plan(entries, false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.ToValidate) != len(entries) {
		t.Errorf("ToValidate = %d; want %d", len(plan.ToValidate), len(entries))
	}
	if len(plan.Stale) != 0 {
		t.Errorf("Stale = %v; want none", plan.Stale)
	}
}

// TestLockGateway_Plan_UpToDate_ReturnsEmpty verifies that an up-to-date lock
// returns no entries for (re)validation.
func TestLockGateway_Plan_UpToDate_ReturnsEmpty(t *testing.T) {
	lockPath := tempLockPath(t)
	clock := fixedClock()
	entries := explicitEntries()

	// Pre-populate the lock with current hashes.
	prior := routinginfra.Lock{Version: routinginfra.LockVersion, Entries: map[string]routinginfra.LockEntry{}}
	for _, entry := range entries {
		prior.Entries[entry.Key()] = routinginfra.LockEntry{SHA256: entry.Hash(), ValidatedAt: clock()}
	}
	if err := routinginfra.WriteLock(lockPath, prior); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	gateway := NewLockGateway(lockPath, clock)
	plan, err := gateway.Plan(entries, false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.ToValidate) != 0 {
		t.Errorf("ToValidate = %v; want empty (up to date)", plan.ToValidate)
	}
}

// TestLockGateway_Plan_Force_ReturnsAllEntries verifies that force=true
// bypasses the on-disk lock and returns every entry.
func TestLockGateway_Plan_Force_ReturnsAllEntries(t *testing.T) {
	lockPath := tempLockPath(t)
	clock := fixedClock()
	entries := explicitEntries()

	// Pre-populate so without force nothing would need validating.
	prior := routinginfra.Lock{Version: routinginfra.LockVersion, Entries: map[string]routinginfra.LockEntry{}}
	for _, entry := range entries {
		prior.Entries[entry.Key()] = routinginfra.LockEntry{SHA256: entry.Hash(), ValidatedAt: clock()}
	}
	if err := routinginfra.WriteLock(lockPath, prior); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	gateway := NewLockGateway(lockPath, clock)
	plan, err := gateway.Plan(entries, true)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.ToValidate) != len(entries) {
		t.Errorf("ToValidate = %d; want %d (force ignores lock)", len(plan.ToValidate), len(entries))
	}
}

// TestLockGateway_Commit_WritesAllSuccessesToLock verifies that Commit writes
// every passing entry into the lock file.
func TestLockGateway_Commit_WritesAllSuccessesToLock(t *testing.T) {
	lockPath := tempLockPath(t)
	gateway := NewLockGateway(lockPath, fixedClock())
	entries := explicitEntries()
	results := []validationdomain.EntryResult{
		passingResult(entries[0]),
		passingResult(entries[1]),
	}

	if err := gateway.Commit(results, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	lock, err := routinginfra.ReadLock(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	for _, entry := range entries {
		lockEntry, ok := lock.Entries[entry.Key()]
		if !ok {
			t.Errorf("lock missing %s", entry.Key())
			continue
		}
		if lockEntry.SHA256 != entry.Hash() {
			t.Errorf("%s SHA256 = %q; want %q", entry.Key(), lockEntry.SHA256, entry.Hash())
		}
	}
}

// TestLockGateway_Commit_PartialFailure_OnlySuccessesEnterLock verifies that
// only passing entries are written to the lock.
func TestLockGateway_Commit_PartialFailure_OnlySuccessesEnterLock(t *testing.T) {
	lockPath := tempLockPath(t)
	gateway := NewLockGateway(lockPath, fixedClock())
	entries := explicitEntries()
	results := []validationdomain.EntryResult{
		failingResult(entries[0]), // acme/api fails
		passingResult(entries[1]), // acme/web passes
	}

	if err := gateway.Commit(results, nil); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	lock, err := routinginfra.ReadLock(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if _, ok := lock.Entries["acme/api"]; ok {
		t.Errorf("acme/api failed; should not be in lock: %+v", lock.Entries)
	}
	if _, ok := lock.Entries["acme/web"]; !ok {
		t.Errorf("acme/web passed; should be in lock: %+v", lock.Entries)
	}
}

// TestLockGateway_Commit_StaleKeysDropped verifies that Commit removes stale
// keys from the lock.
func TestLockGateway_Commit_StaleKeysDropped(t *testing.T) {
	lockPath := tempLockPath(t)
	clock := fixedClock()
	entries := explicitEntries()

	// Seed the lock with both entries plus a stale key.
	prior := routinginfra.Lock{Version: routinginfra.LockVersion, Entries: map[string]routinginfra.LockEntry{
		"acme/api": {SHA256: entries[0].Hash(), ValidatedAt: clock()},
		"acme/web": {SHA256: entries[1].Hash(), ValidatedAt: clock()},
		"acme/old": {SHA256: "deadbeef", ValidatedAt: clock()},
	}}
	if err := routinginfra.WriteLock(lockPath, prior); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	gateway := NewLockGateway(lockPath, clock)
	if err := gateway.Commit(nil, []string{"acme/old"}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	lock, err := routinginfra.ReadLock(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if _, ok := lock.Entries["acme/old"]; ok {
		t.Errorf("stale key acme/old should have been dropped")
	}
	// Existing valid entries should be preserved.
	if _, ok := lock.Entries["acme/api"]; !ok {
		t.Errorf("acme/api should still be in lock after stale drop")
	}
}

// TestLockGateway_CommitTargeted_WritesEntry verifies that CommitTargeted
// writes the single entry into the lock file.
func TestLockGateway_CommitTargeted_WritesEntry(t *testing.T) {
	lockPath := tempLockPath(t)
	gateway := NewLockGateway(lockPath, fixedClock())
	entry := explicitEntries()[0] // acme/api

	if err := gateway.CommitTargeted(entry); err != nil {
		t.Fatalf("CommitTargeted: %v", err)
	}

	lock, err := routinginfra.ReadLock(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	lockEntry, ok := lock.Entries["acme/api"]
	if !ok {
		t.Fatalf("lock missing acme/api: %+v", lock.Entries)
	}
	if lockEntry.SHA256 != entry.Hash() {
		t.Errorf("SHA256 = %q; want %q", lockEntry.SHA256, entry.Hash())
	}
}

// TestLockGateway_CommitTargeted_NoFileBeforeCall verifies that CommitTargeted
// does not require the lock file to already exist.
func TestLockGateway_CommitTargeted_NoFileBeforeCall(t *testing.T) {
	lockPath := tempLockPath(t)
	gateway := NewLockGateway(lockPath, fixedClock())
	entry := explicitEntries()[0]

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file should not exist yet")
	}
	if err := gateway.CommitTargeted(entry); err != nil {
		t.Fatalf("CommitTargeted on missing lock: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lock file should now exist: %v", err)
	}
}
