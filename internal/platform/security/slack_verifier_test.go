package security_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/platform/security"
)

const slackTestSecret = "8f742231b10e8888abcd99yyyzzz85a5"

// slackFixedClock is the reference "now" the Slack verifier tests verify
// against. Slack signs the request timestamp into the base string, so the
// verifier's clock and the timestamp used to sign must agree (within the replay
// window).
var slackFixedClock = time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

func slackVerifierAt(secret string, now time.Time) *security.SlackVerifier {
	return security.NewSlackVerifier(secret, security.WithSlackClock(func() time.Time { return now }))
}

// slackSign builds the "v0=<hex>" Slack signature of body for the given timestamp.
func slackSign(timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(slackTestSecret))
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write(body)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func slackTimestamp(at time.Time) string {
	return strconv.FormatInt(at.Unix(), 10)
}

// compoundSignature builds the "<timestamp>\n<v0=hex>" string the middleware
// passes to SlackVerifier.Verify.
func compoundSignature(timestamp string, body []byte) string {
	return timestamp + "\n" + slackSign(timestamp, body)
}

func TestSlackVerifier_ValidSignature(t *testing.T) {
	verifier := slackVerifierAt(slackTestSecret, slackFixedClock)
	body := []byte(`payload=%7B%22type%22%3A%22block_actions%22%7D`)
	timestamp := slackTimestamp(slackFixedClock)

	err := verifier.Verify(body, compoundSignature(timestamp, body))

	assert.NoError(t, err)
}

func TestSlackVerifier_TamperedBody(t *testing.T) {
	verifier := slackVerifierAt(slackTestSecret, slackFixedClock)
	signature := compoundSignature(slackTimestamp(slackFixedClock), []byte("payload=original"))

	err := verifier.Verify([]byte("payload=tampered"), signature)

	assert.ErrorIs(t, err, security.ErrInvalidSignature)
}

func TestSlackVerifier_WrongSecret(t *testing.T) {
	verifier := slackVerifierAt("a-different-secret", slackFixedClock)
	body := []byte("payload=x")

	err := verifier.Verify(body, compoundSignature(slackTimestamp(slackFixedClock), body))

	assert.ErrorIs(t, err, security.ErrInvalidSignature)
}

func TestSlackVerifier_StaleTimestamp(t *testing.T) {
	verifier := slackVerifierAt(slackTestSecret, slackFixedClock)
	body := []byte("payload=x")
	// Six minutes in the past — outside Slack's 5-minute replay window. The
	// signature itself is valid; staleness alone must reject it.
	stale := slackTimestamp(slackFixedClock.Add(-6 * time.Minute))

	err := verifier.Verify(body, compoundSignature(stale, body))

	assert.ErrorIs(t, err, security.ErrStaleTimestamp)
}

func TestSlackVerifier_FutureTimestampWithinWindow(t *testing.T) {
	verifier := slackVerifierAt(slackTestSecret, slackFixedClock)
	body := []byte("payload=x")
	// Clock skew can place the timestamp slightly ahead of us; the window is
	// two-sided, so a timestamp 1 minute in the future is still accepted.
	future := slackTimestamp(slackFixedClock.Add(1 * time.Minute))

	err := verifier.Verify(body, compoundSignature(future, body))

	assert.NoError(t, err)
}

func TestSlackVerifier_UnparseableTimestamp(t *testing.T) {
	verifier := slackVerifierAt(slackTestSecret, slackFixedClock)
	body := []byte("payload=x")

	err := verifier.Verify(body, "not-a-number\n"+slackSign("not-a-number", body))

	assert.ErrorIs(t, err, security.ErrInvalidSignature)
}

func TestSlackVerifier_BadSignatureScheme(t *testing.T) {
	verifier := slackVerifierAt(slackTestSecret, slackFixedClock)
	body := []byte("payload=x")
	timestamp := slackTimestamp(slackFixedClock)

	signatures := map[string]string{
		"missing scheme": timestamp + "\n" + hex.EncodeToString(make([]byte, 32)),
		"wrong scheme":   timestamp + "\nv1=" + hex.EncodeToString(make([]byte, 32)),
		"empty sig part": timestamp + "\n",
		"not hex":        timestamp + "\nv0=zzzz",
		"truncated":      timestamp + "\nv0=abc",
		"no newline":     slackSign(timestamp, body),
	}
	for name, signature := range signatures {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, verifier.Verify(body, signature), security.ErrInvalidSignature)
		})
	}
}
