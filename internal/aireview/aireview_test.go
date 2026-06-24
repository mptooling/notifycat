package aireview_test

import (
	"testing"

	"github.com/mptooling/notifycat/internal/aireview"
)

func TestDetector_IsBotIdentifiesExactly(t *testing.T) {
	d := aireview.NewDetector()
	if !d.IsBot("Bot") {
		t.Error("IsBot(\"Bot\") = false; want true")
	}
	for _, senderType := range []string{"", "bot", "BOT", "Robot", "User"} {
		if d.IsBot(senderType) {
			t.Errorf("IsBot(%q) = true; want false (only \"Bot\" matches)", senderType)
		}
	}
}
