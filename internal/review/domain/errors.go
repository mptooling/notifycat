package domain

import "errors"

// ErrActiveReviewExists marks a user who already has an in-progress review on the
// PR — a duplicate "Start review" click, which the use case treats as a no-op.
// The infrastructure layer maps the store's unique-violation sentinel to this.
var ErrActiveReviewExists = errors.New("review: active review already exists")
