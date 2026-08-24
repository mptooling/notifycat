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

// HasWarnings reports whether any check warned.
func (r Report) HasWarnings() bool {
	for _, c := range r.Checks {
		if c.Status == StatusWarn {
			return true
		}
	}
	return false
}

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

func (r EntryResult) Cacheable() bool {
	return r.OK() && !r.HasWarnings()
}

type ChannelInfo struct {
	ID         string
	Name       string
	IsMember   bool
	IsArchived bool
}
