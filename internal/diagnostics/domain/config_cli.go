package domain

import (
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// EntrySource provides the current mapping entries to config-CLI use cases.
// The routing Provider satisfies this interface.
type EntrySource interface {
	Entries() []routingdomain.Entry
}

// LockPlan is the result of diffing the mappings against the lock: the entries
// that need (re)validation and the stale lock keys no longer backed by an entry.
type LockPlan struct {
	ToValidate []routingdomain.Entry
	Stale      []string
}

// LockGateway abstracts the config lock file. Plan reads the lock and diffs it
// against the current entries; Commit merges successful validation results into
// the lock (recording each entry's hash + validation time) and drops stale
// keys; CommitTargeted records a single explicit entry's successful validation.
type LockGateway interface {
	Plan(entries []routingdomain.Entry, force bool) (LockPlan, error)
	Commit(successes []validationdomain.EntryResult, stale []string) error
	CommitTargeted(entry routingdomain.Entry) error
}
