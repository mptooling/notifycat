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
)

// String renders Status as OK / FAIL / SKIP for greppable CLI output.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusFail:
		return "FAIL"
	case StatusSkip:
		return "SKIP"
	default:
		return "UNKNOWN"
	}
}
