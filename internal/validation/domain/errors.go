package domain

import "fmt"

// SlackAPIError is a domain-level view of a Slack API error, carrying the
// method and error code the application interprets into an operator-facing
// remediation message. The validation infrastructure layer translates the
// platform Slack client's own API error into this type so the application can
// classify failures without importing the Slack SDK.
type SlackAPIError struct {
	Method string
	Code   string
}

// Error renders the method and code, matching the platform client's format.
func (e *SlackAPIError) Error() string {
	return fmt.Sprintf("slack: %s: %s", e.Method, e.Code)
}
