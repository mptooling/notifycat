package domain

import "errors"

// Access sentinel errors.
var (
	ErrUnauthorized        = errors.New("access: unauthorized")
	ErrMalformedCredential = errors.New("access: malformed credential")
	ErrInvalidUser         = errors.New("access: invalid user")
	ErrInviteNotFound      = errors.New("access: invite not found")
	ErrInviteUsed          = errors.New("access: invite already used")
	ErrTokenNotFound       = errors.New("access: token not found")
)

// AuthError is an authentication failure carrying the reason for logs; it unwraps to ErrUnauthorized.
type AuthError struct {
	Reason AuthFailureReason
}

func (authError *AuthError) Error() string {
	return ErrUnauthorized.Error() + ": " + string(authError.Reason)
}

func (authError *AuthError) Unwrap() error { return ErrUnauthorized }
