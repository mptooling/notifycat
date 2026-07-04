package domain

import "errors"

// ErrNoActiveReview marks the absence of an in-progress review session for a PR.
// The reaction handlers treat it as "no session to finish", not a failure.
var ErrNoActiveReview = errors.New("notification: no active review session")
