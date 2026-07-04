package slackhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/mptooling/notifycat/internal/platform/security"
)

const testSecret = "8f742231b10e8888abcd99yyyzzz85a5"

// fixedClock is the reference "now" the tests verify against. Slack signs the
// request timestamp into the base string, so the verifier's clock and the
// timestamp used to sign must agree (within the replay window).
var fixedClock = time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

func clockAt(t time.Time) security.SlackOption {
	return security.WithSlackClock(func() time.Time { return t })
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
