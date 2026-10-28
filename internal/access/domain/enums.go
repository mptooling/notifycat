package domain

// AuthFailureReason names why authentication failed, for logs only.
type AuthFailureReason string

// Authentication failure reasons.
const (
	AuthFailureMalformed AuthFailureReason = "malformed"
	AuthFailureUnknown   AuthFailureReason = "unknown"
	AuthFailureMismatch  AuthFailureReason = "mismatch"
	AuthFailureExpired   AuthFailureReason = "expired"
	AuthFailureUsed      AuthFailureReason = "used"
)

// ErrorCode is the machine-readable code in an HTTP error body.
type ErrorCode string

// HTTP error codes.
const (
	ErrorCodeUnauthorized ErrorCode = "unauthorized"
	ErrorCodeInvalidBody  ErrorCode = "invalid_body"
	ErrorCodeInvalidUser  ErrorCode = "invalid_user"
	ErrorCodeNotFound     ErrorCode = "not_found"
	ErrorCodeInternal     ErrorCode = "internal_error"
)
