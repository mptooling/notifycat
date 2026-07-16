package domain

import "fmt"

// RateLimitedError reports a provider 429/quota response. Detail carries the
// provider's own error text (for doctor and logs); RetryAfter is the
// Retry-After header value when the provider sent one.
type RateLimitedError struct {
	Detail     string
	RetryAfter string
}

func (e *RateLimitedError) Error() string {
	if e.RetryAfter == "" {
		return fmt.Sprintf("model provider rate limited: %s", e.Detail)
	}
	return fmt.Sprintf("model provider rate limited (retry after %s): %s", e.RetryAfter, e.Detail)
}
