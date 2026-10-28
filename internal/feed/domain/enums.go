package domain

// ErrorCode is the machine-readable code in an HTTP error body.
type ErrorCode string

// HTTP error codes.
const (
	ErrorCodeUnauthorized         ErrorCode = "unauthorized"
	ErrorCodeInvalidBody          ErrorCode = "invalid_body"
	ErrorCodeUnknownRepository    ErrorCode = "unknown_repository"
	ErrorCodeTooManySubscriptions ErrorCode = "too_many_subscriptions"
	ErrorCodeInvalidSince         ErrorCode = "invalid_since"
	ErrorCodeNotFound             ErrorCode = "not_found"
	ErrorCodeInternal             ErrorCode = "internal_error"
	ErrorCodeMethodNotAllowed     ErrorCode = "method_not_allowed"
)

// CleanupReason says why stored pull requests were removed at startup.
type CleanupReason string

// Startup cleanup reasons.
const (
	CleanupReasonAgentAPIOff CleanupReason = "agent_api_off"
	CleanupReasonUnmapped    CleanupReason = "unmapped"
)
