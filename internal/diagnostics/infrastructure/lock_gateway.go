package infrastructure

import (
	"fmt"
	"io"
	"time"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	routinginfra "github.com/mptooling/notifycat/internal/routing/infrastructure"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// LockGateway implements diagnosticsdomain.LockGateway over the routing
// infrastructure lock-store functions. lockPath is the path to the .lock file
// that lives next to mappings.yaml; clock is used to stamp ValidatedAt on each
// entry that passes validation.
type LockGateway struct {
	lockPath string
	clock    func() time.Time
}

// NewLockGateway returns a LockGateway for the given lock file path and clock.
func NewLockGateway(lockPath string, clock func() time.Time) *LockGateway {
	return &LockGateway{lockPath: lockPath, clock: clock}
}

// Plan reads the on-disk lock and diffs it against entries. When force is true
// the on-disk lock is ignored and every entry is returned for (re)validation.
// A missing lock file is not an error — it returns all entries for validation.
// A malformed lock file is also non-fatal; the caller should log the warning
// (Plan surfaces the error so the caller can do so) but all entries still
// validate.
func (g *LockGateway) Plan(entries []routingdomain.Entry, force bool) (diagnosticsdomain.LockPlan, error) {
	if force {
		return diagnosticsdomain.LockPlan{ToValidate: entries}, nil
	}
	lock, err := routinginfra.ReadLock(g.lockPath)
	diff := routinginfra.DiffEntries(entries, lock)
	return diagnosticsdomain.LockPlan{ToValidate: diff.Needs, Stale: diff.Stale}, err
}

// Commit writes the merged lock: the existing lock is read, successful
// validation results are merged in (keyed by entry hash + current time), and
// stale keys are dropped.
func (g *LockGateway) Commit(successes []validationdomain.EntryResult, stale []string) error {
	lock, _ := routinginfra.ReadLock(g.lockPath)
	merged := routinginfra.MergeLock(lock, successMap(successes, g.clock), stale)
	return routinginfra.WriteLock(g.lockPath, merged)
}

// CommitTargeted reads the current lock, sets the single entry's LockEntry
// (SHA256 + ValidatedAt), and writes it back. Used after a targeted validation
// pass on an explicit (non-wildcard) entry.
func (g *LockGateway) CommitTargeted(entry routingdomain.Entry) error {
	lock, _ := routinginfra.ReadLock(g.lockPath)
	merged := routinginfra.MergeLock(lock,
		map[string]routinginfra.LockEntry{
			entry.Key(): {SHA256: entry.Hash(), ValidatedAt: g.clock()},
		}, nil)
	return routinginfra.WriteLock(g.lockPath, merged)
}

// successMap builds the map of key → LockEntry for results that passed validation.
func successMap(results []validationdomain.EntryResult, clock func() time.Time) map[string]routinginfra.LockEntry {
	out := map[string]routinginfra.LockEntry{}
	for _, result := range results {
		if result.OK() {
			out[result.Entry.Key()] = routinginfra.LockEntry{
				SHA256:      result.Entry.Hash(),
				ValidatedAt: clock(),
			}
		}
	}
	return out
}

// WriteLockWarning formats the warning message emitted when Plan returns a
// non-nil error (malformed lock). It writes to stderr and returns the formatted
// string so callers do not need to know the exact phrasing.
func WriteLockWarning(stderr io.Writer, err error) {
	fmt.Fprintln(stderr, "validate: warning:", err, "(rebuilding lock)")
}
