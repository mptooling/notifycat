package domain

import routingdomain "github.com/mptooling/notifycat/internal/routing/domain"

// CheckResult is one row of a Report.
type CheckResult struct {
	Name   string
	Status Status
	Detail string
}

// Report aggregates the per-check results for a single mapping.
type Report struct {
	Repository string
	Checks     []CheckResult
}

// OK returns true when no check failed. Skipped checks do not count as
// failures.
func (r Report) OK() bool {
	for _, c := range r.Checks {
		if c.Status == StatusFail {
			return false
		}
	}
	return true
}

// HasWarnings reports whether any check warned. A warning never fails a
// report; it only keeps the entry out of the lock.
func (r Report) HasWarnings() bool {
	for _, c := range r.Checks {
		if c.Status == StatusWarn {
			return true
		}
	}
	return false
}

// EntryResult bundles every report produced for a single mapping entry, so
// callers can update the lock per-entry: an entry is "validated" only when
// every report it produced is OK, and it may only be cached when it also
// produced no warnings — see Cacheable.
type EntryResult struct {
	Entry   routingdomain.Entry
	Reports []Report
}

// OK reports whether every contributed report passed.
func (r EntryResult) OK() bool {
	for _, rep := range r.Reports {
		if !rep.OK() {
			return false
		}
	}
	return true
}

// HasWarnings reports whether any contributed report warned.
func (r EntryResult) HasWarnings() bool {
	for _, rep := range r.Reports {
		if rep.HasWarnings() {
			return true
		}
	}
	return false
}

// Cacheable reports whether this entry's outcome may be written to
// config.lock. Warned entries are deliberately excluded: an entry with an
// unconfirmed or missing webhook must be re-probed — and its warning
// re-logged — on every boot until the operator resolves it.
func (r EntryResult) Cacheable() bool {
	return r.OK() && !r.HasWarnings()
}

// ChannelInfo is the subset of a Slack channel's metadata the validator needs
// to confirm the bot can post: the channel's identity, whether the bot is a
// member, and whether it is archived. It mirrors the platform Slack client's
// own ChannelInfo; the validation infrastructure layer maps between the two so
// the domain stays free of the Slack SDK.
type ChannelInfo struct {
	ID         string
	Name       string
	IsMember   bool
	IsArchived bool
}
