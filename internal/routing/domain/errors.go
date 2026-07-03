package domain

import "errors"

// ErrNotFound is returned when a repository resolves to no mapping. It is the
// routing provider's port-contract sentinel. The message string is retained
// verbatim ("store: not found") so store.ErrNotFound can alias this value
// during the migration without changing any error text or errors.Is behaviour.
var ErrNotFound = errors.New("store: not found")
