package security_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/platform/security"
)

const slackTestSecret = "8f742231b10e8888abcd99yyyzzz85a5"

// slackFixedClock is the reference "now" the Slack verifier tests verify
// against. Slack signs the request timestamp into the base string, so the
// verifier's clock and the timestamp used to sign must agree (within the replay
// window).
var slackFixedClock = time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

func slackClockAt(t time.Time) security.SlackOption {
	return security.WithSlackClock(func() time.Time { return t })
}

// slackSign builds the "v0=<hex>" Slack signature of body for the given
// timestamp.
func slackSign(timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(slackTestSecret))
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write(body)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func slackTSString(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10)
}

// compoundSig builds the compound "<timestamp>\n<v0=hex>" string the middleware
// passes to SlackVerifier.Verify.
func compoundSig(timestamp string, body []byte) string {
	return timestamp + "\n" + slackSign(timestamp, body)
}

func TestSlackVerifier_ValidSignature(t *testing.T) {
	v := security.NewSlackVerifier(slackTestSecret, slackClockAt(slackFixedClock))
	body := []byte(`payload=%7B%22type%22%3A%22block_actions%22%7D`)
	ts := slackTSString(slackFixedClock)

	if err := v.Verify(body, compoundSig(ts, body)); err != nil {
		t.Fatalf("Verify with valid signature returned %v; want nil", err)
	}
}

func TestSlackVerifier_TamperedBody(t *testing.T) {
	v := security.NewSlackVerifier(slackTestSecret, slackClockAt(slackFixedClock))
	ts := slackTSString(slackFixedClock)
	sig := compoundSig(ts, []byte("payload=original"))

	if err := v.Verify([]byte("payload=tampered"), sig); !errors.Is(err, security.ErrInvalidSignature) {
		t.Fatalf("Verify with tampered body = %v; want ErrInvalidSignature", err)
	}
}

func TestSlackVerifier_WrongSecret(t *testing.T) {
	v := security.NewSlackVerifier("a-different-secret", slackClockAt(slackFixedClock))
	body := []byte("payload=x")
	ts := slackTSString(slackFixedClock)

	if err := v.Verify(body, compoundSig(ts, body)); !errors.Is(err, security.ErrInvalidSignature) {
		t.Fatalf("Verify with wrong secret = %v; want ErrInvalidSignature", err)
	}
}

func TestSlackVerifier_StaleTimestamp(t *testing.T) {
	v := security.NewSlackVerifier(slackTestSecret, slackClockAt(slackFixedClock))
	body := []byte("payload=x")

	// Six minutes in the past — outside Slack's 5-minute replay window. The
	// signature itself is valid; staleness alone must reject it.
	stale := slackFixedClock.Add(-6 * time.Minute)
	ts := slackTSString(stale)

	if err := v.Verify(body, compoundSig(ts, body)); !errors.Is(err, security.ErrStaleTimestamp) {
		t.Fatalf("Verify with stale timestamp = %v; want ErrStaleTimestamp", err)
	}
}

func TestSlackVerifier_FutureTimestampWithinWindow(t *testing.T) {
	v := security.NewSlackVerifier(slackTestSecret, slackClockAt(slackFixedClock))
	body := []byte("payload=x")

	// Clock skew can place the timestamp slightly ahead of us; the window is
	// two-sided, so a timestamp 1 minute in the future is still accepted.
	future := slackFixedClock.Add(1 * time.Minute)
	ts := slackTSString(future)

	if err := v.Verify(body, compoundSig(ts, body)); err != nil {
		t.Fatalf("Verify with near-future timestamp returned %v; want nil", err)
	}
}

func TestSlackVerifier_UnparseableTimestamp(t *testing.T) {
	v := security.NewSlackVerifier(slackTestSecret, slackClockAt(slackFixedClock))
	body := []byte("payload=x")
	sig := "not-a-number\n" + slackSign("not-a-number", body)

	if err := v.Verify(body, sig); !errors.Is(err, security.ErrInvalidSignature) {
		t.Fatalf("Verify with unparseable timestamp = %v; want ErrInvalidSignature", err)
	}
}

func TestSlackVerifier_BadSignatureScheme(t *testing.T) {
	v := security.NewSlackVerifier(slackTestSecret, slackClockAt(slackFixedClock))
	body := []byte("payload=x")
	ts := slackTSString(slackFixedClock)

	cases := map[string]string{
		"missing scheme": ts + "\n" + hex.EncodeToString(make([]byte, 32)),
		"wrong scheme":   ts + "\n" + "v1=" + hex.EncodeToString(make([]byte, 32)),
		"empty sig part": ts + "\n",
		"not hex":        ts + "\n" + "v0=zzzz",
		"truncated":      ts + "\n" + "v0=abc",
		"no newline":     slackSign(ts, body),
	}
	for name, sig := range cases {
		t.Run(name, func(t *testing.T) {
			if err := v.Verify(body, sig); !errors.Is(err, security.ErrInvalidSignature) {
				t.Fatalf("Verify(%q) = %v; want ErrInvalidSignature", sig, err)
			}
		})
	}
}
