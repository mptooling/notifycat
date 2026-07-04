// Package infrastructure holds the validation adapters that satisfy the
// domain ports over real clients — today, a Slack probe over the platform
// Slack client. It is the only validation layer permitted to import an SDK
// client package.
package infrastructure
