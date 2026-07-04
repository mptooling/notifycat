// Package application holds the validation use cases: the per-repository
// Validator and the entry runner that expands and validates mapping entries.
// It orchestrates the domain ports and contains all validation business
// logic; it touches no SDK, database, or network directly.
package application
