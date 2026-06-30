// Package slackhook authenticates and parses inbound Slack interactivity
// requests. It exposes a constant-time HMAC verifier (with Slack's replay
// window), an HTTP middleware that gates a downstream handler, and a parser for
// the interaction envelope. It mirrors internal/githubhook; the differences are
// Slack's signing scheme (a timestamped base string) and the form-encoded body.
package slackhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

// SignatureHeader carries Slack's "v0=<hex>" request signature.
const SignatureHeader = "X-Slack-Signature"

// TimestampHeader carries the Unix-seconds timestamp Slack signed into the
// base string. It is part of the signed payload, so trusting it for replay
// protection is safe once the signature checks out.
const TimestampHeader = "X-Slack-Request-Timestamp"

// signaturePrefix is the only signature scheme Slack uses today.
const signaturePrefix = "v0="

// DefaultMaxAge is Slack's documented replay window: requests whose timestamp
// is more than five minutes from now (in either direction, to absorb clock
// skew) are rejected before the HMAC is checked.
const DefaultMaxAge = 5 * time.Minute

// ErrInvalidSignature is returned when the signature does not match the body.
var ErrInvalidSignature = errors.New("slackhook: invalid signature")

// ErrStaleTimestamp is returned when the request timestamp falls outside the
// replay window. Kept distinct from ErrInvalidSignature so callers and tests
// can tell a replayed (or badly clock-skewed) request from a forged one, even
// though both map to 401 at the HTTP layer.
var ErrStaleTimestamp = errors.New("slackhook: stale timestamp")

// Verifier checks Slack request signatures against a shared signing secret and
// enforces the replay window.
type Verifier struct {
	secret []byte
	now    func() time.Time
	maxAge time.Duration
}

// Option configures a Verifier.
type Option func(*Verifier)

// WithClock overrides the time source used for replay-window checks. Tests
// inject a fixed clock; production uses time.Now.
func WithClock(now func() time.Time) Option {
	return func(v *Verifier) { v.now = now }
}

// WithMaxAge overrides the replay window. Defaults to DefaultMaxAge.
func WithMaxAge(d time.Duration) Option {
	return func(v *Verifier) { v.maxAge = d }
}

// NewVerifier returns a Verifier configured with the given signing secret.
func NewVerifier(secret string, opts ...Option) *Verifier {
	v := &Verifier{secret: []byte(secret), now: time.Now, maxAge: DefaultMaxAge}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// Verify checks that signature is a valid "v0=<hex>" HMAC of Slack's base
// string ("v0:{timestamp}:{rawBody}") under the verifier's secret, and that
// timestamp is within the replay window.
//
// The staleness check runs first — the timestamp is part of the signed base
// string, so a forged-but-fresh timestamp still fails the HMAC. The HMAC
// comparison runs in constant time to prevent timing oracles.
//
// Returns ErrStaleTimestamp for a timestamp outside the window and
// ErrInvalidSignature for any signature mismatch or malformed input.
func (v *Verifier) Verify(timestamp string, body []byte, signature string) error {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrInvalidSignature
	}
	age := v.now().Sub(time.Unix(seconds, 0))
	if math.Abs(age.Seconds()) > v.maxAge.Seconds() {
		return ErrStaleTimestamp
	}

	if !strings.HasPrefix(signature, signaturePrefix) {
		return ErrInvalidSignature
	}
	provided, err := hex.DecodeString(signature[len(signaturePrefix):])
	if err != nil {
		return ErrInvalidSignature
	}

	mac := hmac.New(sha256.New, v.secret)
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write(body)
	expected := mac.Sum(nil)

	if !hmac.Equal(provided, expected) {
		return ErrInvalidSignature
	}
	return nil
}
