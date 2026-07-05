package domain

import (
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// Section is a named group of preflight checks the doctor emits.
type Section struct {
	Name   string
	Checks []validationdomain.CheckResult
}

// OK reports whether the section passed: a skipped check does not fail it.
func (s Section) OK() bool {
	for _, check := range s.Checks {
		if check.Status == validationdomain.StatusFail {
			return false
		}
	}
	return true
}

// ConfigSnapshot carries the facts the doctor validates about the runtime
// configuration — never raw secret values, only whether each is set. Built by
// the infrastructure layer from config.Config so the application stays free of
// config/store imports.
type ConfigSnapshot struct {
	ConfigFile     string
	DatabaseURL    string
	Domain         string
	MessageTTLDays int
	// WebhookSecretSet reports whether the selected git provider's webhook secret
	// is set; WebhookSecretVar names that env var (e.g. GITHUB_WEBHOOK_SECRET,
	// BITBUCKET_WEBHOOK_SECRET) for the report.
	WebhookSecretSet bool
	WebhookSecretVar string
	SlackTokenSet    bool
	// TokenSet reports whether the selected provider's optional read token is set;
	// TokenVar names that env var (GITHUB_TOKEN, BITBUCKET_TOKEN).
	TokenSet         bool
	TokenVar         string
	DatabaseOpenable bool
	DatabaseDetail   string
	Entries          []routingdomain.Entry
	HasPathRules     bool
}
