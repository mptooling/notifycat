// Package mappings owns the declarative repository → Slack-channel
// configuration: parsing mappings.yaml, the in-memory Provider used at
// runtime, and the mappings.lock cache that records which entries have been
// validated. The package replaces the database-backed RepoMappings store.
package mappings
