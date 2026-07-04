package security

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

// SlackSignatureHeader carries Slack's "v0=<hex>" request signature.
const SlackSignatureHeader = "X-Slack-Signature"

// SlackTimestampHeader carries the Unix-seconds timestamp Slack signed into the
// base string. It is part of the signed payload, so trusting it for replay
// protection is safe once the signature checks out.
const SlackTimestampHeader = "X-Slack-Request-Timestamp"

// slackSignaturePrefix is the only signature scheme Slack uses today.
const slackSignaturePrefix = "v0="

// SlackDefaultMaxAge is Slack's documented replay window: requests whose
// timestamp is more than five minutes from now (in either direction, to absorb
// clock skew) are rejected before the HMAC is checked.
const SlackDefaultMaxAge = 5 * time.Minute

// ErrStaleTimestamp is returned when the request timestamp falls outside the
// replay window. Kept distinct from ErrInvalidSignature so callers and tests
// can tell a replayed (or badly clock-skewed) request from a forged one, even
// though both map to 401 at the HTTP layer.
var ErrStaleTimestamp = errors.New("security: stale timestamp")

// SlackVerifier checks Slack request signatures against a shared signing secret
// and enforces the replay window.
//
// To satisfy SignatureVerifier, Verify accepts a compound signature of the form
// "<unix-timestamp>\n<v0=hex>" — the slackhook middleware builds this string
// from the two Slack request headers before calling Verify.
type SlackVerifier struct {
	secret []byte
	now    func() time.Time
	maxAge time.Duration
}

// SlackOption configures a SlackVerifier.
type SlackOption func(*SlackVerifier)

// WithSlackClock overrides the time source used for replay-window checks. Tests
// inject a fixed clock; production uses time.Now.
func WithSlackClock(now func() time.Time) SlackOption {
	return func(v *SlackVerifier) { v.now = now }
}

// WithSlackMaxAge overrides the replay window. Defaults to SlackDefaultMaxAge.
func WithSlackMaxAge(d time.Duration) SlackOption {
	return func(v *SlackVerifier) { v.maxAge = d }
}

// NewSlackVerifier returns a SlackVerifier configured with the given signing
// secret.
func NewSlackVerifier(secret string, opts ...SlackOption) *SlackVerifier {
	v := &SlackVerifier{secret: []byte(secret), now: time.Now, maxAge: SlackDefaultMaxAge}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// Verify checks that sig is a valid compound Slack signature of the form
// "<unix-timestamp>\n<v0=hex>". It checks that the timestamp is within the
// replay window and that the HMAC of Slack's base string
// ("v0:{timestamp}:{rawBody}") matches.
//
// The staleness check runs first — the timestamp is part of the signed base
// string, so a forged-but-fresh timestamp still fails the HMAC. The HMAC
// comparison runs in constant time to prevent timing oracles.
//
// Returns ErrStaleTimestamp for a timestamp outside the window and
// ErrInvalidSignature for any signature mismatch or malformed input.
func (v *SlackVerifier) Verify(body []byte, sig string) error {
	newline := strings.IndexByte(sig, '\n')
	if newline < 0 {
		return ErrInvalidSignature
	}
	timestamp := sig[:newline]
	signature := sig[newline+1:]

	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrInvalidSignature
	}
	age := v.now().Sub(time.Unix(seconds, 0))
	if math.Abs(age.Seconds()) > v.maxAge.Seconds() {
		return ErrStaleTimestamp
	}

	if !strings.HasPrefix(signature, slackSignaturePrefix) {
		return ErrInvalidSignature
	}
	provided, err := hex.DecodeString(signature[len(slackSignaturePrefix):])
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

var _ SignatureVerifier = (*SlackVerifier)(nil)
