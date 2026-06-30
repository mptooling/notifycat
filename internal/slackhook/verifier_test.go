package slackhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/slackhook"
)

const testSecret = "8f742231b10e8888abcd99yyyzzz85a5"

// fixedClock is the reference "now" the tests verify against. Slack signs the
// request timestamp into the base string, so the verifier's clock and the
// timestamp used to sign must agree (within the replay window).
var fixedClock = time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

func clockAt(t time.Time) slackhook.Option {
	return slackhook.WithClock(func() time.Time { return t })
}

// sign builds the "v0=<hex>" Slack signature of body for the given timestamp.
func sign(timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write(body)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func tsString(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10)
}

func TestVerifier_ValidSignature(t *testing.T) {
	v := slackhook.NewVerifier(testSecret, clockAt(fixedClock))
	body := []byte(`payload=%7B%22type%22%3A%22block_actions%22%7D`)
	ts := tsString(fixedClock)

	if err := v.Verify(ts, body, sign(ts, body)); err != nil {
		t.Fatalf("Verify with valid signature returned %v; want nil", err)
	}
}

func TestVerifier_TamperedBody(t *testing.T) {
	v := slackhook.NewVerifier(testSecret, clockAt(fixedClock))
	ts := tsString(fixedClock)
	signature := sign(ts, []byte("payload=original"))

	if err := v.Verify(ts, []byte("payload=tampered"), signature); !errors.Is(err, slackhook.ErrInvalidSignature) {
		t.Fatalf("Verify with tampered body = %v; want ErrInvalidSignature", err)
	}
}

func TestVerifier_WrongSecret(t *testing.T) {
	v := slackhook.NewVerifier("a-different-secret", clockAt(fixedClock))
	body := []byte("payload=x")
	ts := tsString(fixedClock)

	if err := v.Verify(ts, body, sign(ts, body)); !errors.Is(err, slackhook.ErrInvalidSignature) {
		t.Fatalf("Verify with wrong secret = %v; want ErrInvalidSignature", err)
	}
}

func TestVerifier_StaleTimestamp(t *testing.T) {
	v := slackhook.NewVerifier(testSecret, clockAt(fixedClock))
	body := []byte("payload=x")

	// Six minutes in the past — outside Slack's 5-minute replay window. The
	// signature itself is valid; staleness alone must reject it.
	stale := fixedClock.Add(-6 * time.Minute)
	ts := tsString(stale)

	if err := v.Verify(ts, body, sign(ts, body)); !errors.Is(err, slackhook.ErrStaleTimestamp) {
		t.Fatalf("Verify with stale timestamp = %v; want ErrStaleTimestamp", err)
	}
}

func TestVerifier_FutureTimestampWithinWindow(t *testing.T) {
	v := slackhook.NewVerifier(testSecret, clockAt(fixedClock))
	body := []byte("payload=x")

	// Clock skew can place the timestamp slightly ahead of us; the window is
	// two-sided, so a timestamp 1 minute in the future is still accepted.
	future := fixedClock.Add(1 * time.Minute)
	ts := tsString(future)

	if err := v.Verify(ts, body, sign(ts, body)); err != nil {
		t.Fatalf("Verify with near-future timestamp returned %v; want nil", err)
	}
}

func TestVerifier_UnparseableTimestamp(t *testing.T) {
	v := slackhook.NewVerifier(testSecret, clockAt(fixedClock))
	body := []byte("payload=x")

	if err := v.Verify("not-a-number", body, sign("not-a-number", body)); !errors.Is(err, slackhook.ErrInvalidSignature) {
		t.Fatalf("Verify with unparseable timestamp = %v; want ErrInvalidSignature", err)
	}
}

func TestVerifier_BadSignatureScheme(t *testing.T) {
	v := slackhook.NewVerifier(testSecret, clockAt(fixedClock))
	body := []byte("payload=x")
	ts := tsString(fixedClock)

	cases := map[string]string{
		"missing scheme": hex.EncodeToString(make([]byte, 32)),
		"wrong scheme":   "v1=" + hex.EncodeToString(make([]byte, 32)),
		"empty":          "",
		"not hex":        "v0=zzzz",
		"truncated":      "v0=abc",
	}
	for name, signature := range cases {
		t.Run(name, func(t *testing.T) {
			if err := v.Verify(ts, body, signature); !errors.Is(err, slackhook.ErrInvalidSignature) {
				t.Fatalf("Verify(%q) = %v; want ErrInvalidSignature", signature, err)
			}
		})
	}
}
