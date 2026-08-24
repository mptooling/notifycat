package domain

// Status is the outcome of a single check.
type Status int

const (
	// StatusOK means the check passed.
	StatusOK Status = iota
	// StatusFail means the check found a problem the operator must fix.
	StatusFail
	// StatusSkip means the check could not run (e.g., GitHub token absent).
	StatusSkip
	// StatusWarn means the check found an actionable problem that limits what
	// notifycat can do for this entry, but that must not block startup.
	StatusWarn
)

// String renders Status as OK / FAIL / SKIP / WARN for greppable CLI output.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusFail:
		return "FAIL"
	case StatusSkip:
		return "SKIP"
	case StatusWarn:
		return "WARN"
	default:
		return "UNKNOWN"
	}
}
