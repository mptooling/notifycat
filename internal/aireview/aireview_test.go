package aireview_test

import (
	"testing"

	"github.com/mptooling/notifycat/internal/aireview"
)

func TestDetector_DisabledNeverSuppresses(t *testing.T) {
	d := aireview.NewDetector(false)
	for _, senderType := range []string{"Bot", "User", "", "bot", "BOT"} {
		if d.ShouldSuppress(senderType) {
			t.Errorf("disabled detector suppressed senderType=%q; want false", senderType)
		}
	}
}

func TestDetector_EnabledSuppressesBotExactly(t *testing.T) {
	d := aireview.NewDetector(true)
	if !d.ShouldSuppress("Bot") {
		t.Error("enabled detector did not suppress senderType=\"Bot\"")
	}
}

func TestDetector_EnabledDoesNotSuppressUser(t *testing.T) {
	d := aireview.NewDetector(true)
	if d.ShouldSuppress("User") {
		t.Error("enabled detector suppressed senderType=\"User\"")
	}
}

func TestDetector_EnabledIsCaseSensitive(t *testing.T) {
	// GitHub's payload uses the exact string "Bot". Anything else (lowercase
	// "bot", empty, unrelated) must not be treated as a bot.
	d := aireview.NewDetector(true)
	for _, senderType := range []string{"", "bot", "BOT", "Robot", "User"} {
		if d.ShouldSuppress(senderType) {
			t.Errorf("enabled detector suppressed senderType=%q; want false (only \"Bot\" matches)", senderType)
		}
	}
}
